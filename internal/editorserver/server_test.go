package editorserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cvpp/internal/appdata"
	"cvpp/internal/erp"
)

func TestBootstrapCookieAndSecretFreeStatus(t *testing.T) {
	dataDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := make(chan string, 1)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- Serve(ctx, Options{Addr: "127.0.0.1:0", DataDir: dataDir, AppMode: true, OpenURL: func(value string) error { opened <- value; return nil }})
	}()
	var bootstrap string
	select {
	case bootstrap = <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not start")
	}
	parsed, err := url.Parse(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusFound {
		t.Fatalf("bootstrap status %d", response.StatusCode)
	}
	cookie := response.Cookies()[0]
	if cookie.Name != "cvpp_session" || !cookie.HttpOnly {
		t.Fatalf("unexpected cookie %#v", cookie)
	}
	response.Body.Close()
	request, _ := http.NewRequest(http.MethodGet, parsed.Scheme+"://"+parsed.Host+"/api/app/status?token=should-not-work", nil)
	request.AddCookie(cookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("query token status %d", response.StatusCode)
	}
	response.Body.Close()
	request, _ = http.NewRequest(http.MethodGet, parsed.Scheme+"://"+parsed.Host+"/api/app/status", nil)
	request.Host = "evil.example"
	request.AddCookie(cookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected host status %d", response.StatusCode)
	}
	response.Body.Close()
	request, _ = http.NewRequest(http.MethodGet, parsed.Scheme+"://"+parsed.Host+"/api/app/status", nil)
	request.AddCookie(cookie)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status code %d: %s", response.StatusCode, data)
	}
	var status map[string]any
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatal(err)
	}
	encoded := string(data)
	if strings.Contains(encoded, "password") || strings.Contains(encoded, "answer") || strings.Contains(encoded, "roll_number") {
		t.Fatalf("status leaked secret fields: %s", encoded)
	}
	shutdown, _ := http.NewRequest(http.MethodPost, parsed.Scheme+"://"+parsed.Host+"/api/app/shutdown", strings.NewReader("{}"))
	shutdown.Header.Set("Content-Type", "application/json")
	shutdown.AddCookie(cookie)
	_, _ = http.DefaultClient.Do(shutdown)
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestJobStatusRetainsImportCompletionForPollingClients(t *testing.T) {
	server := &Server{events: newEventHub()}
	runner := &jobRunner{server: server}

	jobID, err := runner.reserve("import")
	if err != nil {
		t.Fatal(err)
	}
	if jobID == 0 {
		t.Fatal("job ID was not assigned")
	}
	runner.phase("otp-required")
	running := runner.status()
	if !running.Running || running.Completed || running.Kind != "import" || running.Phase != "otp-required" {
		t.Fatalf("unexpected running job state: %#v", running)
	}

	runner.finish(true, "import complete", "")
	completed := runner.status()
	if completed.ID != jobID || completed.Running || !completed.Completed || !completed.OK || completed.Message != "import complete" {
		t.Fatalf("unexpected completed job state: %#v", completed)
	}
}

func TestFriendlyErrorDoesNotGuessSessionExpiry(t *testing.T) {
	message := "new ERP session is invalid: ERP returned an unexpected page"
	if got := friendlyError(errors.New(message)); got != message {
		t.Fatalf("friendlyError() = %q, want original diagnostic", got)
	}

	want := "ERP did not keep this login active. Try again with the newest OTP. Your local resume was not changed."
	if got := friendlyError(erp.ErrSessionRejected); got != want {
		t.Fatalf("friendlyError(session rejection) = %q, want %q", got, want)
	}
}

func TestShutdownWaitsForLogoutAndReportsSuccess(t *testing.T) {
	secrets := t.TempDir()
	if err := os.WriteFile(filepath.Join(secrets, ".session"), []byte("ssoToken=test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logoutRequested := make(chan struct{}, 1)
	erpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/IIT_ERP3/logout.htm" {
			http.NotFound(writer, request)
			return
		}
		logoutRequested <- struct{}{}
		fmt.Fprint(writer, "logged out")
	}))
	defer erpServer.Close()
	client, _ := erp.New(erpServer.URL, secrets)
	keeper := newSessionKeeper(time.Hour)
	keeper.SetClient(client)
	shutdown := make(chan struct{}, 1)
	server := &Server{options: Options{BaseURL: erpServer.URL, SecretsDir: secrets}, keepAlive: keeper, shutdown: func() { shutdown <- struct{}{} }}

	request := httptest.NewRequest(http.MethodPost, "/api/app/shutdown", nil)
	response := httptest.NewRecorder()
	server.handleShutdown(response, request)
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["logoutAttempted"] != true || payload["logoutOK"] != true || payload["logoutError"] != nil {
		t.Fatalf("shutdown response = %#v", payload)
	}
	select {
	case <-logoutRequested:
	default:
		t.Fatal("shutdown returned before requesting ERP logout")
	}
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("server did not shut down after logout")
	}
}

func TestShutdownReportsLogoutFailureAndStillCloses(t *testing.T) {
	secrets := t.TempDir()
	if err := os.WriteFile(filepath.Join(secrets, ".session"), []byte("ssoToken=test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	erpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer erpServer.Close()
	client, _ := erp.New(erpServer.URL, secrets)
	keeper := newSessionKeeper(time.Hour)
	keeper.SetClient(client)
	shutdown := make(chan struct{}, 1)
	server := &Server{options: Options{BaseURL: erpServer.URL, SecretsDir: secrets}, keepAlive: keeper, shutdown: func() { shutdown <- struct{}{} }}

	response := httptest.NewRecorder()
	server.handleShutdown(response, httptest.NewRequest(http.MethodPost, "/api/app/shutdown", nil))
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["logoutOK"] != false || !strings.Contains(fmt.Sprint(payload["logoutError"]), "503") {
		t.Fatalf("shutdown response = %#v", payload)
	}
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("server stayed open after failed ERP logout")
	}
}

func TestAppShutsDownAfterWindowDisconnectGracePeriod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shutdown := make(chan struct{}, 1)
	server := &Server{lastActivity: time.Now(), jobs: &jobRunner{}, shutdown: func() { shutdown <- struct{}{} }}
	go server.stopWhenIdleAfter(ctx, 5*time.Millisecond, 20*time.Millisecond)
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("app did not shut down after its window disconnected")
	}
}

func TestPDFViewerUsesNonceAndVersionedLocalFile(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "/pdf/cv1", nil)
	response := httptest.NewRecorder()
	server.handlePDFViewer(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("viewer status = %d", response.Code)
	}
	body := response.Body.String()
	csp := response.Header().Get("Content-Security-Policy")
	marker := "script-src 'nonce-"
	start := strings.Index(csp, marker)
	if start < 0 {
		t.Fatalf("viewer CSP has no script nonce: %q", csp)
	}
	start += len(marker)
	end := strings.Index(csp[start:], "'")
	if end < 0 {
		t.Fatalf("viewer CSP has an invalid script nonce: %q", csp)
	}
	nonce := csp[start : start+end]
	if strings.Count(body, `nonce="`+nonce+`"`) != 2 {
		t.Fatal("viewer style and script must use the CSP nonce")
	}
	if !strings.Contains(csp, "frame-src 'self'") || !strings.Contains(body, `fileURL + "/" + encodeURIComponent(signature)`) {
		t.Fatal("viewer must render the versioned local PDF file")
	}
	if strings.Contains(body, "toolbar=0") {
		t.Fatal("viewer must keep the browser PDF toolbar")
	}
}

func TestVersionedPDFPathSelectsCV(t *testing.T) {
	variant, err := cvFromPath("/pdf/file/cv2/123:456/789", "/pdf/file/cv")
	if err != nil {
		t.Fatal(err)
	}
	if variant != 2 {
		t.Fatalf("variant = %d, want 2", variant)
	}
}

func TestAppModeERPAndPreviewSharePDFPath(t *testing.T) {
	paths, err := appdata.Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{options: Options{AppMode: true}, paths: paths}
	want := filepath.Join(paths.PDFDir, "resume-erp-cv1.pdf")
	if got := server.pdfPath(1); got != want {
		t.Fatalf("PDF path = %q, want %q", got, want)
	}
}
