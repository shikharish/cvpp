package editorserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

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
