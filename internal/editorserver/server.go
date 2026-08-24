package editorserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cvpp/editor"
	"cvpp/internal/appdata"
	"cvpp/internal/defaultdata"
	"cvpp/internal/erp"
	"cvpp/internal/progress"
	"cvpp/internal/resumedata"
	"cvpp/internal/workflow"
)

type Options struct {
	RepoRoot   string
	Addr       string
	JSONPath   string
	SecretsDir string
	BaseURL    string
	OpenURL    func(string) error
	OpenERPURL func(string) error
	DataDir    string
	AppMode    bool
}

var version = "dev"

const (
	shutdownLogoutTimeout = 15 * time.Second
	appIdleCheckInterval  = time.Second
	appIdleShutdownDelay  = 5 * time.Second
)

type Server struct {
	options         Options
	paths           appdata.Paths
	host            string
	bootstrapToken  string
	sessionToken    string
	bootstrapUsed   bool
	mu              sync.Mutex
	connected       int
	lastActivity    time.Time
	shutdown        context.CancelFunc
	instance        *appdata.Instance
	otp             *erp.InteractiveOTP
	securityAnswer  *erp.InteractiveSecurityAnswer
	keepAlive       *sessionKeeper
	events          *eventHub
	jobs            *jobRunner
	logoutOnce      sync.Once
	logoutAttempted bool
	logoutErr       error
}

func Serve(ctx context.Context, options Options) error {
	if options.Addr == "" {
		options.Addr = "127.0.0.1:0"
	}
	paths, err := appdata.Resolve(options.DataDir)
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	if options.AppMode || options.DataDir != "" {
		if options.JSONPath == "" {
			options.JSONPath = paths.ResumeJSON
		}
		if options.SecretsDir == "" {
			options.SecretsDir = paths.SecretsDir
		}
	} else {
		if options.JSONPath == "" {
			options.JSONPath = "data/resume.json"
		}
		if options.SecretsDir == "" {
			options.SecretsDir = ".erp-cv-secrets"
		}
	}

	listener, err := net.Listen("tcp", options.Addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	bootstrapToken, err := appdata.RandomToken(24)
	if err != nil {
		return err
	}
	sessionToken, err := appdata.RandomToken(32)
	if err != nil {
		return err
	}
	server := &Server{options: options, paths: paths, host: listener.Addr().String(), bootstrapToken: bootstrapToken, sessionToken: sessionToken, events: newEventHub(), otp: erp.NewInteractiveOTP(), securityAnswer: erp.NewInteractiveSecurityAnswer(), lastActivity: time.Now()}
	server.jobs = &jobRunner{server: server}
	server.keepAlive = newSessionKeeper(10 * time.Minute)
	instance, err := appdata.AcquireInstance(paths, appdata.RuntimeState{PID: os.Getpid(), Port: listener.Addr().(*net.TCPAddr).Port, StartedAt: time.Now().UTC(), URL: "http://" + listener.Addr().String() + "/"})
	if err != nil {
		return err
	}
	defer server.logoutERP()
	server.instance = instance
	defer instance.Close()
	defer server.securityAnswer.Close()

	mux := http.NewServeMux()
	server.routes(mux)

	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server.shutdown = cancel
	go server.keepAlive.Run(requestCtx, func() bool { return server.jobs.isRunning() }, func() (*erp.Client, error) {
		client, clientErr := erp.New(server.options.BaseURL, server.resolve(server.options.SecretsDir))
		if clientErr != nil {
			return nil, clientErr
		}
		restoreCtx, restoreCancel := context.WithTimeout(requestCtx, 45*time.Second)
		defer restoreCancel()
		if restoreErr := client.RestoreSavedSession(restoreCtx); restoreErr != nil {
			return nil, restoreErr
		}
		return client, nil
	})
	httpServer := &http.Server{Handler: securityHeaders(server, mux)}
	errc := make(chan error, 1)
	go func() {
		<-requestCtx.Done()
		_ = httpServer.Shutdown(context.Background())
	}()
	if options.AppMode {
		go server.stopWhenIdle(requestCtx)
	}
	go func() {
		errc <- httpServer.Serve(listener)
	}()

	url := "http://" + listener.Addr().String() + "/bootstrap?token=" + bootstrapToken
	progress.Logf("Editor: serving %s", url)
	if options.OpenURL != nil {
		progress.Logf("Editor: opening the browser")
		if err := options.OpenURL(url); err != nil {
			return fmt.Errorf("open editor: %w", err)
		}
	}
	progress.Logf("Editor: press Ctrl-C to stop the server")

	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("/bootstrap", s.handleBootstrap)
	mux.HandleFunc("/api/app/status", s.requireToken(s.handleAppStatus))
	mux.HandleFunc("/api/app/shutdown", s.requireToken(s.handleShutdown))
	mux.HandleFunc("/api/setup/security-question", s.requireToken(s.handleSecurityQuestion))
	mux.HandleFunc("/api/setup/credentials", s.requireToken(s.handleCredentials))
	mux.HandleFunc("/api/setup/import", s.requireToken(s.handleImport))
	mux.HandleFunc("/api/erp/otp", s.requireToken(s.handleOTP))
	mux.HandleFunc("/api/erp/security-answer", s.requireToken(s.handleSecurityAnswer))
	mux.HandleFunc("/api/resume", s.requireToken(s.handleResume))
	mux.HandleFunc("/api/erp/open", s.requireToken(s.handleERPOpen))
	mux.HandleFunc("/api/erp/run", s.requireToken(s.handleERPRun))
	mux.HandleFunc("/api/erp/events", s.requireToken(s.handleERPEvents))
	mux.HandleFunc("/api/pdf/status", s.requireToken(s.handlePDFStatus))
	mux.HandleFunc("/pdf/cv1", s.requireToken(s.handlePDFViewer))
	mux.HandleFunc("/pdf/cv2", s.requireToken(s.handlePDFViewer))
	mux.HandleFunc("/pdf/cv3", s.requireToken(s.handlePDFViewer))
	mux.HandleFunc("/pdf/file/cv1", s.requireToken(s.handlePDFFile))
	mux.HandleFunc("/pdf/file/cv1/", s.requireToken(s.handlePDFFile))
	mux.HandleFunc("/pdf/file/cv2", s.requireToken(s.handlePDFFile))
	mux.HandleFunc("/pdf/file/cv2/", s.requireToken(s.handlePDFFile))
	mux.HandleFunc("/pdf/file/cv3", s.requireToken(s.handlePDFFile))
	mux.HandleFunc("/pdf/file/cv3/", s.requireToken(s.handlePDFFile))

	mux.Handle("/", http.FileServer(http.FS(editor.Files)))
}

func (s *Server) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "" || r.Header.Get("X-Resume-Editor-Token") != "" {
			http.Error(w, "query-string tokens are not accepted; open the bootstrap URL once", http.StatusForbidden)
			return
		}
		cookie, err := r.Cookie("cvpp_session")
		if err != nil || cookie.Value != s.sessionToken {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Query().Get("token") == "" || r.URL.Query().Get("token") != s.bootstrapToken {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	if s.bootstrapUsed {
		s.mu.Unlock()
		http.Error(w, "bootstrap token expired", http.StatusGone)
		return
	}
	s.bootstrapUsed = true
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "cvpp_session", Value: s.sessionToken, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	http.Redirect(w, r, "/", http.StatusFound)
}

func securityHeaders(s *Server, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if host := r.Host; host != s.host && host != "localhost:"+strings.Split(s.host, ":")[1] {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.host && origin != "http://localhost:"+strings.Split(s.host, ":")[1] {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		s.mu.Lock()
		s.connected++
		s.lastActivity = time.Now()
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.connected--
			if s.connected == 0 {
				s.lastActivity = time.Now()
			}
			s.mu.Unlock()
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) stopWhenIdle(ctx context.Context) {
	s.stopWhenIdleAfter(ctx, appIdleCheckInterval, appIdleShutdownDelay)
}

func (s *Server) stopWhenIdleAfter(ctx context.Context, checkInterval, idleDelay time.Duration) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.mu.Lock()
			idle := s.connected == 0 && now.Sub(s.lastActivity) >= idleDelay
			s.mu.Unlock()
			if idle && !s.jobs.isRunning() {
				if s.shutdown != nil {
					s.shutdown()
				}
				return
			}
		}
	}
}

func (s *Server) resumePath() string {
	if s.options.JSONPath != "" {
		return s.resolve(s.options.JSONPath)
	}
	return s.paths.ResumeJSON
}

func (s *Server) secretsDir() string {
	if s.options.SecretsDir != "" {
		return s.resolve(s.options.SecretsDir)
	}
	return s.paths.SecretsDir
}

func (s *Server) handleAppStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resumeExists := false
	if _, err := os.Stat(s.resumePath()); err == nil {
		resumeExists = true
	}
	_, credentialErr := os.Stat(filepath.Join(s.secretsDir(), "erpcreds.json"))
	_, sessionErr := os.Stat(filepath.Join(s.secretsDir(), ".session"))
	pdfs := make([]map[string]any, 0, 3)
	for variant := 1; variant <= 3; variant++ {
		path := s.pdfPath(variant)
		item := map[string]any{"cv": variant, "exists": false}
		if info, err := os.Stat(path); err == nil {
			item["exists"] = true
			item["size"] = info.Size()
			item["signature"] = fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
		}
		pdfs = append(pdfs, item)
	}
	job := s.jobs.status()
	securityQuestion, securityAnswerRequired := s.securityAnswer.Current()
	writeJSON(w, map[string]any{
		"ok": true, "version": version, "onboarding": !resumeExists || credentialErr != nil,
		"credentials": credentialErr == nil, "session": sessionErr == nil, "resume": resumeExists,
		"pdfs": pdfs, "jobRunning": job.Running, "job": job, "otpRequired": s.otp.Waiting(),
		"securityAnswerRequired": securityAnswerRequired, "securityQuestion": securityQuestion,
	})
}

func (s *Server) handleSecurityQuestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		RollNumber string `json:"rollNumber"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&request); err != nil {
		writeAPIError(w, errors.New("invalid request"), http.StatusBadRequest)
		return
	}
	client, err := erp.New(s.options.BaseURL, s.secretsDir())
	if err != nil {
		writeAPIError(w, err, http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	question, err := client.FetchSecurityQuestion(ctx, request.RollNumber)
	if err != nil {
		writeAPIError(w, err, http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "question": question})
}

func (s *Server) handleCredentials(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(s.secretsDir(), "erpcreds.json")
	switch r.Method {
	case http.MethodPut:
		var request erp.Credentials
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&request); err != nil {
			writeAPIError(w, errors.New("invalid credentials payload"), http.StatusBadRequest)
			return
		}
		if existing, readErr := erp.LoadCredentials(path); readErr == nil {
			if request.RollNumber == "" {
				request.RollNumber = existing.RollNumber
			}
			if request.Password == "" {
				request.Password = existing.Password
			}
			if request.Answers == nil {
				request.Answers = map[string]string{}
			}
			for question, answer := range existing.Answers {
				if _, present := request.Answers[question]; !present {
					request.Answers[question] = answer
				}
			}
		}
		if err := erp.SaveCredentials(path, request); err != nil {
			writeAPIError(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "saved": true})
	case http.MethodDelete:
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			writeAPIError(w, err, http.StatusInternalServerError)
			return
		}
		if err := os.Remove(filepath.Join(s.secretsDir(), ".session")); err != nil && !os.IsNotExist(err) {
			writeAPIError(w, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "forgotten": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		OTP string `json:"otp"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&request); err != nil {
		writeAPIError(w, errors.New("invalid OTP payload"), http.StatusBadRequest)
		return
	}
	if err := s.otp.Submit(request.OTP); err != nil {
		writeAPIError(w, err, http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "accepted": true})
}

func (s *Server) handleSecurityAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&request); err != nil {
		writeAPIError(w, errors.New("invalid security answer payload"), http.StatusBadRequest)
		return
	}
	if err := s.securityAnswer.Submit(request.Answer); err != nil {
		writeAPIError(w, err, http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "accepted": true})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		FreshLogin bool `json:"freshLogin"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(w, errors.New("invalid import payload"), http.StatusBadRequest)
		return
	}
	jobID, err := s.jobs.startImport(request.FreshLogin)
	if err != nil {
		writeAPIError(w, err, http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "started": true, "jobID": jobID})
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	attempted, logoutErr := s.logoutERP()
	response := map[string]any{"ok": true, "shuttingDown": true, "logoutAttempted": attempted, "logoutOK": logoutErr == nil}
	if logoutErr != nil {
		response["logoutError"] = logoutErr.Error()
	}
	writeJSON(w, response)
	if s.shutdown != nil {
		go s.shutdown()
	}
}

func (s *Server) logoutERP() (bool, error) {
	s.logoutOnce.Do(func() {
		defer func() {
			if s.logoutErr != nil {
				progress.Logf("ERP session: logout failed; CV++ will still close (%v)", s.logoutErr)
			}
		}()
		var client *erp.Client
		if s.keepAlive != nil {
			client = s.keepAlive.current()
		}
		secretsDir := s.secretsDir()
		if client == nil && secretsDir == "" {
			return
		}

		needsRestore := client == nil
		if needsRestore {
			if _, err := os.Stat(filepath.Join(secretsDir, ".session")); err != nil {
				if os.IsNotExist(err) {
					return
				}
				s.logoutAttempted = true
				s.logoutErr = fmt.Errorf("inspect saved ERP session: %w", err)
				return
			}
			s.logoutAttempted = true
			var err error
			client, err = erp.New(s.options.BaseURL, secretsDir)
			if err != nil {
				s.logoutErr = fmt.Errorf("prepare ERP logout: %w", err)
				return
			}
		} else {
			s.logoutAttempted = true
		}

		ctx, cancel := context.WithTimeout(context.Background(), shutdownLogoutTimeout)
		defer cancel()
		if needsRestore {
			err := client.RestoreSavedSession(ctx)
			if err != nil {
				if errors.Is(err, erp.ErrSessionRejected) {
					s.logoutErr = client.DiscardSavedSession()
					return
				}
				s.logoutErr = fmt.Errorf("restore ERP session for logout: %w", err)
				return
			}
		}

		s.logoutErr = client.Logout(ctx)
		if s.logoutErr == nil && s.keepAlive != nil {
			s.keepAlive.clear(client)
		}
	})
	return s.logoutAttempted, s.logoutErr
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(s.resumePath())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				data, err = defaultdata.Files.ReadFile("resume.json")
			}
			if err != nil {
				writeAPIError(w, err, http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(data)
	case http.MethodPut:
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			writeAPIError(w, err, http.StatusBadRequest)
			return
		}
		var resume resumedata.Resume
		if err := json.Unmarshal(data, &resume); err != nil {
			writeAPIError(w, fmt.Errorf("parse resume JSON: %w", err), http.StatusBadRequest)
			return
		}
		if err := resume.Validate(); err != nil {
			writeAPIError(w, err, http.StatusBadRequest)
			return
		}
		if !bytes.HasSuffix(data, []byte("\n")) {
			data = append(data, '\n')
		}
		if err := appdata.AtomicWrite(s.resumePath(), data, 0o600); err != nil {
			writeAPIError(w, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleERPRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		CV           int  `json:"cv"`
		FreshLogin   bool `json:"freshLogin"`
		DownloadOnly bool `json:"downloadOnly"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(w, err, http.StatusBadRequest)
		return
	}
	if request.CV == 0 {
		request.CV = 1
	}
	if request.CV < 1 || request.CV > 3 {
		writeAPIError(w, fmt.Errorf("cv must be 1, 2, or 3"), http.StatusBadRequest)
		return
	}
	jobID, err := s.jobs.start(request.CV, request.FreshLogin, request.DownloadOnly)
	if err != nil {
		writeAPIError(w, err, http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "started": true, "jobID": jobID})
}

func (s *Server) handleERPOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.options.OpenERPURL == nil {
		writeAPIError(w, fmt.Errorf("opening ERP from this editor process is not configured"), http.StatusInternalServerError)
		return
	}
	var request struct {
		FreshLogin bool `json:"freshLogin"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(w, err, http.StatusBadRequest)
		return
	}
	jobID, err := s.jobs.startOpen(request.FreshLogin)
	if err != nil {
		writeAPIError(w, err, http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "started": true, "jobID": jobID})
}

func (s *Server) handleERPEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := s.events.subscribe()
	defer unsubscribe()
	s.events.write(w, event{name: "status", data: map[string]any{"running": s.jobs.isRunning()}})
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			s.events.write(w, event)
			flusher.Flush()
		}
	}
}

func (s *Server) handlePDFStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	variant, err := parseCV(r.URL.Query().Get("cv"))
	if err != nil {
		writeAPIError(w, err, http.StatusBadRequest)
		return
	}
	setNoCache(w)
	s.writePDFStatus(w, variant)
}

func (s *Server) handlePDFViewer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	variant, err := cvFromPath(r.URL.Path, "/pdf/cv")
	if err != nil {
		writeAPIError(w, err, http.StatusBadRequest)
		return
	}
	nonce, err := appdata.RandomToken(16)
	if err != nil {
		writeAPIError(w, err, http.StatusInternalServerError)
		return
	}
	setNoCache(w)
	w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-src 'self'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"'; object-src 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>CV%d PDF viewer</title>
  <style nonce="%s">
    :root { color-scheme: light; font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; min-width: 900px; color: #172033; background: #101828; }
    header { height: 44px; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 0 14px; color: white; background: #183153; border-bottom: 1px solid #6f9ee9; }
    h1 { margin: 0; font-size: 15px; }
    #status { color: #d9e5ff; font-size: 12px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    iframe { display: block; width: 100vw; height: calc(100vh - 45px); border: 0; background: #525659; }
  </style>
</head>
<body>
  <header>
    <h1>CV%d PDF</h1>
    <div id="status">Waiting for pdf/resume-erp-cv%d.pdf…</div>
  </header>
  <iframe id="pdf" title="CV%d PDF"></iframe>
  <script nonce="%s">
    const cv = %d;
    const statusURL = "/api/pdf/status?cv=%d";
    const fileURL = "/pdf/file/cv%d";
    const status = document.getElementById("status");
    const frame = document.getElementById("pdf");
    let currentSignature = "";
    let polling = false;

    async function poll() {
      if (polling) return;
      polling = true;
      try {
        const response = await fetch(statusURL, { cache: "no-store" });
        if (!response.ok) throw new Error(response.status + " " + response.statusText);
        const payload = await response.json();
        if (!payload.exists) {
          status.textContent = "Waiting for " + payload.path + "…";
          return;
        }
        const signature = payload.signature || (payload.modTime + ":" + payload.size);
        if (signature !== currentSignature) {
          currentSignature = signature;
          frame.src = fileURL + "/" + encodeURIComponent(signature) + "/" + Date.now() + "#view=Fit&zoom=page-fit";
        }
        const updated = payload.modTime ? new Date(payload.modTime).toLocaleTimeString() : "unknown time";
        status.textContent = "Watching " + payload.path + " · " + payload.size + " bytes · updated " + updated;
      } catch (error) {
        status.textContent = "PDF watcher error: " + (error && error.message ? error.message : error);
      } finally {
        polling = false;
      }
    }

    poll();
    setInterval(poll, 1000);
  </script>
</body>
</html>`, variant, nonce, variant, variant, variant, nonce, variant, variant, variant)
}

func (s *Server) handlePDFFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	variant, err := cvFromPath(r.URL.Path, "/pdf/file/cv")
	if err != nil {
		writeAPIError(w, err, http.StatusBadRequest)
		return
	}
	path := s.pdfPath(variant)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		writeAPIError(w, err, http.StatusInternalServerError)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		writeAPIError(w, err, http.StatusInternalServerError)
		return
	}
	setNoCache(w)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="resume-erp-cv%d.pdf"`, variant))
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), file)
}

func (s *Server) pdfPath(variant int) string {
	if s.options.DataDir != "" || s.options.AppMode {
		return s.paths.PDF(variant)
	}
	return s.resolve(fmt.Sprintf("pdf/resume-erp-cv%d.pdf", variant))
}

func (s *Server) writePDFStatus(w http.ResponseWriter, variant int) {
	path := s.pdfPath(variant)
	relativePath := filepath.ToSlash(filepath.Join("pdf", fmt.Sprintf("resume-erp-cv%d.pdf", variant)))
	stat, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, map[string]any{
				"ok":     true,
				"exists": false,
				"cv":     variant,
				"path":   relativePath,
			})
			return
		}
		writeAPIError(w, err, http.StatusInternalServerError)
		return
	}
	signature := fmt.Sprintf("%d:%d", stat.ModTime().UnixNano(), stat.Size())
	writeJSON(w, map[string]any{
		"ok":        true,
		"exists":    true,
		"cv":        variant,
		"path":      relativePath,
		"size":      stat.Size(),
		"modTime":   stat.ModTime().Format("2006-01-02T15:04:05.000Z07:00"),
		"signature": signature,
	})
}

func (s *Server) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(s.options.RepoRoot, path)
}

func parseCV(input string) (int, error) {
	variant, err := strconv.Atoi(input)
	if err != nil || variant < 1 || variant > 3 {
		return 0, fmt.Errorf("cv must be 1, 2, or 3")
	}
	return variant, nil
}

func cvFromPath(path, prefix string) (int, error) {
	if !strings.HasPrefix(path, prefix) {
		return 0, fmt.Errorf("cv must be 1, 2, or 3")
	}
	value := strings.TrimPrefix(path, prefix)
	if separator := strings.IndexByte(value, '/'); separator >= 0 {
		value = value[:separator]
	}
	return parseCV(value)
}

func setNoCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

type jobRunner struct {
	server *Server
	mu     sync.Mutex
	nextID uint64
	state  jobState
}

type jobState struct {
	ID        uint64 `json:"id"`
	Kind      string `json:"kind,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Running   bool   `json:"running"`
	Completed bool   `json:"completed"`
	OK        bool   `json:"ok"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (j *jobRunner) reserve(kind string) (uint64, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state.Running {
		return 0, fmt.Errorf("an ERP job is already running")
	}
	j.nextID++
	j.state = jobState{ID: j.nextID, Kind: kind, Phase: "queued", Running: true}
	return j.state.ID, nil
}

func (j *jobRunner) finish(ok bool, message, errorMessage string) {
	j.mu.Lock()
	j.state.Running = false
	j.state.Completed = true
	j.state.OK = ok
	j.state.Message = message
	j.state.Error = errorMessage
	jobID := j.state.ID
	j.mu.Unlock()
	j.server.events.broadcast(event{name: "done", data: map[string]any{
		"jobID": jobID, "ok": ok, "message": message, "error": errorMessage, "running": false,
	}})
}

func (j *jobRunner) start(cv int, freshLogin, downloadOnly bool) (uint64, error) {
	jobID, err := j.reserve("erp")
	if err != nil {
		return 0, err
	}

	go func() {
		j.phase("authenticating")

		label := "sync + download"
		if downloadOnly {
			label = "download-only"
		}
		j.server.events.broadcast(event{name: "log", data: map[string]any{"message": fmt.Sprintf("Starting ERP %s for CV%d", label, cv)}})
		removeSink := progress.AddSink(func(line string) {
			j.server.events.broadcast(event{name: "log", data: map[string]any{"message": line}})
		})
		defer removeSink()

		err := workflow.RunERP(context.Background(), j.server.options.RepoRoot, workflow.ERPOptions{
			Variant:       cv,
			JSONPath:      j.server.options.JSONPath,
			Output:        j.server.pdfPath(cv),
			SecretsDir:    j.server.options.SecretsDir,
			BaseURL:       j.server.options.BaseURL,
			DownloadOnly:  downloadOnly,
			FreshLogin:    freshLogin,
			OTP:           j.otpForJob(),
			Answers:       j.securityAnswersForJob(),
			Phase:         j.phase,
			Authenticated: j.server.keepAlive.SetClient,
		})
		if err != nil {
			j.server.events.broadcast(event{name: "log", data: map[string]any{"message": "ERP failed: " + err.Error()}})
			j.finish(false, "", friendlyError(err))
			return
		}
		j.finish(true, fmt.Sprintf("ERP CV%d PDF updated locally.", cv), "")
	}()
	return jobID, nil
}

func (j *jobRunner) startImport(freshLogin bool) (uint64, error) {
	jobID, err := j.reserve("import")
	if err != nil {
		return 0, err
	}
	go func() {
		removeSink := progress.AddSink(func(line string) {
			j.server.events.broadcast(event{name: "log", data: map[string]any{"message": line}})
		})
		defer removeSink()
		err := workflow.ImportPortal(context.Background(), workflow.ImportOptions{Paths: j.server.paths, BaseURL: j.server.options.BaseURL, FreshLogin: freshLogin, OTP: j.server.otp, Answers: j.server.securityAnswer, Phase: j.phase, Authenticated: j.server.keepAlive.SetClient})
		if err != nil {
			j.finish(false, "", friendlyError(err))
			return
		}
		j.finish(true, "ERP resume imported without changing the portal.", "")
	}()
	return jobID, nil
}

func (j *jobRunner) otpForJob() erp.OTPProvider {
	if j.server.options.AppMode {
		return j.server.otp
	}
	return nil
}

func (j *jobRunner) securityAnswersForJob() erp.SecurityAnswerProvider {
	if j.server.options.AppMode {
		return j.server.securityAnswer
	}
	return nil
}

func (j *jobRunner) phase(value string) {
	j.mu.Lock()
	j.state.Phase = value
	j.mu.Unlock()
	j.server.events.broadcast(event{name: "phase", data: map[string]any{"phase": value, "running": true}})
}

func (j *jobRunner) startOpen(freshLogin bool) (uint64, error) {
	jobID, err := j.reserve("open")
	if err != nil {
		return 0, err
	}

	go func() {
		j.server.events.broadcast(event{name: "log", data: map[string]any{"message": "Starting ERP browser open"}})
		removeSink := progress.AddSink(func(line string) {
			j.server.events.broadcast(event{name: "log", data: map[string]any{"message": line}})
		})
		defer removeSink()

		err := workflow.OpenERPBrowser(context.Background(), j.server.options.RepoRoot, workflow.ERPBrowserOptions{
			SecretsDir:    j.server.options.SecretsDir,
			BaseURL:       j.server.options.BaseURL,
			FreshLogin:    freshLogin,
			OpenURL:       j.server.options.OpenERPURL,
			OTP:           j.otpForJob(),
			Answers:       j.securityAnswersForJob(),
			Phase:         j.phase,
			Authenticated: j.server.keepAlive.SetClient,
		})
		if err != nil {
			j.server.events.broadcast(event{name: "log", data: map[string]any{"message": "ERP open failed: " + err.Error()}})
			j.finish(false, "", friendlyError(err))
			return
		}
		j.finish(true, "ERP browser handoff opened.", "")
	}()
	return jobID, nil
}

func (j *jobRunner) isRunning() bool {
	return j.status().Running
}

func (j *jobRunner) status() jobState {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state
}

func friendlyError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if strings.Contains(strings.ToLower(message), "security question") {
		return "ERP needs the answer to a new security question. Update your local login and retry."
	}
	if errors.Is(err, erp.ErrSessionRejected) {
		return "ERP did not keep this login active. Try again with the newest OTP. Your local resume was not changed."
	}
	if strings.Contains(strings.ToLower(message), "empty pdf") {
		return "ERP returned no PDF. Your local resume is safe; retry later."
	}
	return message
}

type event struct {
	name string
	data any
}

type eventHub struct {
	mu          sync.Mutex
	subscribers map[chan event]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subscribers: map[chan event]struct{}{}}
}

func (h *eventHub) subscribe() (chan event, func()) {
	ch := make(chan event, 32)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subscribers, ch)
		close(ch)
		h.mu.Unlock()
	}
}

func (h *eventHub) broadcast(event event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (h *eventHub) write(w io.Writer, event event) {
	payload, err := json.Marshal(event.data)
	if err != nil {
		payload = []byte(`{"message":"unable to serialize event"}`)
	}
	fmt.Fprintf(w, "event: %s\n", event.name)
	fmt.Fprintf(w, "data: %s\n\n", payload)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    false,
		"error": err.Error(),
	})
}
