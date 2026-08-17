package editorserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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
}

type Server struct {
	options Options
	token   string
	events  *eventHub
	jobs    *jobRunner
}

func Serve(ctx context.Context, options Options) error {
	if options.Addr == "" {
		options.Addr = "127.0.0.1:0"
	}
	if options.JSONPath == "" {
		options.JSONPath = "data/resume.json"
	}
	if options.SecretsDir == "" {
		options.SecretsDir = ".erp-cv-secrets"
	}

	token, err := randomToken()
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", options.Addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &Server{
		options: options,
		token:   token,
		events:  newEventHub(),
	}
	server.jobs = &jobRunner{server: server}

	mux := http.NewServeMux()
	server.routes(mux)

	httpServer := &http.Server{Handler: mux}
	errc := make(chan error, 1)
	go func() {
		<-ctx.Done()
		_ = httpServer.Shutdown(context.Background())
	}()
	go func() {
		errc <- httpServer.Serve(listener)
	}()

	url := "http://" + listener.Addr().String() + "/?token=" + token
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
	mux.HandleFunc("/api/resume", s.requireToken(s.handleResume))
	mux.HandleFunc("/api/erp/open", s.requireToken(s.handleERPOpen))
	mux.HandleFunc("/api/erp/run", s.requireToken(s.handleERPRun))
	mux.HandleFunc("/api/erp/events", s.requireToken(s.handleERPEvents))
	mux.HandleFunc("/api/pdf/status", s.requireToken(s.handlePDFStatus))
	mux.HandleFunc("/pdf/cv1", s.requireToken(s.handlePDFViewer))
	mux.HandleFunc("/pdf/cv2", s.requireToken(s.handlePDFViewer))
	mux.HandleFunc("/pdf/cv3", s.requireToken(s.handlePDFViewer))
	mux.HandleFunc("/pdf/file/cv1", s.requireToken(s.handlePDFFile))
	mux.HandleFunc("/pdf/file/cv2", s.requireToken(s.handlePDFFile))
	mux.HandleFunc("/pdf/file/cv3", s.requireToken(s.handlePDFFile))

	editorDir := filepath.Join(s.options.RepoRoot, "editor")
	mux.Handle("/", http.FileServer(http.Dir(editorDir)))
}

func (s *Server) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			token = r.Header.Get("X-Resume-Editor-Token")
		}
		if token != s.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(s.resolve(s.options.JSONPath))
		if err != nil {
			writeAPIError(w, err, http.StatusInternalServerError)
			return
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
		if err := atomicWrite(s.resolve(s.options.JSONPath), data, 0o644); err != nil {
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
	if err := s.jobs.start(request.CV, request.FreshLogin, request.DownloadOnly); err != nil {
		writeAPIError(w, err, http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "started": true})
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
	if err := s.jobs.startOpen(request.FreshLogin); err != nil {
		writeAPIError(w, err, http.StatusConflict)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "started": true})
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
	setNoCache(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	token := url.QueryEscape(s.token)
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>CV%d PDF viewer</title>
  <style>
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
  <script>
    const cv = %d;
    const statusURL = "/api/pdf/status?cv=%d&token=%s";
    const fileURL = "/pdf/file/cv%d?token=%s";
    const status = document.getElementById("status");
    const frame = document.getElementById("pdf");
    let currentSignature = "";

    async function poll() {
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
          frame.src = fileURL + "&v=" + encodeURIComponent(signature);
        }
        const updated = payload.modTime ? new Date(payload.modTime).toLocaleTimeString() : "unknown time";
        status.textContent = "Watching " + payload.path + " · " + payload.size + " bytes · updated " + updated;
      } catch (error) {
        status.textContent = "PDF watcher error: " + (error && error.message ? error.message : error);
      }
    }

    poll();
    setInterval(poll, 1000);
  </script>
</body>
</html>`, variant, variant, variant, variant, variant, variant, token, variant, token)
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
	return parseCV(strings.TrimPrefix(path, prefix))
}

func setNoCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

type jobRunner struct {
	server *Server
	mu     sync.Mutex
	active bool
}

func (j *jobRunner) reserve() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.active {
		return fmt.Errorf("an ERP job is already running")
	}
	j.active = true
	return nil
}

func (j *jobRunner) release() {
	j.mu.Lock()
	j.active = false
	j.mu.Unlock()
}

func (j *jobRunner) start(cv int, freshLogin, downloadOnly bool) error {
	if err := j.reserve(); err != nil {
		return err
	}

	go func() {
		defer j.release()

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
			Variant:      cv,
			JSONPath:     j.server.options.JSONPath,
			SecretsDir:   j.server.options.SecretsDir,
			BaseURL:      j.server.options.BaseURL,
			DownloadOnly: downloadOnly,
			FreshLogin:   freshLogin,
		})
		if err != nil {
			j.server.events.broadcast(event{name: "log", data: map[string]any{"message": "ERP failed: " + err.Error()}})
			j.server.events.broadcast(event{name: "done", data: map[string]any{"ok": false, "error": err.Error(), "running": false}})
			return
		}
		output := "pdf/resume-erp-cv" + strconv.Itoa(cv) + ".pdf"
		j.server.events.broadcast(event{name: "done", data: map[string]any{"ok": true, "message": "ERP PDF saved to " + output, "running": false}})
	}()
	return nil
}

func (j *jobRunner) startOpen(freshLogin bool) error {
	if err := j.reserve(); err != nil {
		return err
	}

	go func() {
		defer j.release()

		j.server.events.broadcast(event{name: "log", data: map[string]any{"message": "Starting ERP browser open"}})
		removeSink := progress.AddSink(func(line string) {
			j.server.events.broadcast(event{name: "log", data: map[string]any{"message": line}})
		})
		defer removeSink()

		err := workflow.OpenERPBrowser(context.Background(), j.server.options.RepoRoot, workflow.ERPBrowserOptions{
			SecretsDir: j.server.options.SecretsDir,
			BaseURL:    j.server.options.BaseURL,
			FreshLogin: freshLogin,
			OpenURL:    j.server.options.OpenERPURL,
		})
		if err != nil {
			j.server.events.broadcast(event{name: "log", data: map[string]any{"message": "ERP open failed: " + err.Error()}})
			j.server.events.broadcast(event{name: "done", data: map[string]any{"ok": false, "error": err.Error(), "running": false}})
			return
		}
		j.server.events.broadcast(event{name: "done", data: map[string]any{"ok": true, "message": "ERP browser handoff opened.", "running": false}})
	}()
	return nil
}

func (j *jobRunner) isRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.active
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

func randomToken() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".resume-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
