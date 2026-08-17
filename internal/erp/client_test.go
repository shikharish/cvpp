package erp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type staticOTP string

func (staticOTP) Prepare(context.Context) error              { return nil }
func (value staticOTP) Wait(context.Context) (string, error) { return string(value), nil }

type countingOTP struct {
	prepareCalls int
	waitCalls    int
}

func (provider *countingOTP) Prepare(context.Context) error {
	provider.prepareCalls++
	return nil
}

func (provider *countingOTP) Wait(context.Context) (string, error) {
	provider.waitCalls++
	return "123456", nil
}

func TestOTPRequestAccepted(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{"An OTP has been sent", true},
		{" An OTP (valid for a short time) has been sent to your email. ", true},
		{"Unable to send OTP. Make sure answare of the security question has been provided.", false},
		{"OTP could not be sent", false},
		{"", false},
	}
	for _, test := range tests {
		if got := otpRequestAccepted(test.message); got != test.want {
			t.Errorf("otpRequestAccepted(%q) = %v, want %v", test.message, got, test.want)
		}
	}
}

func TestAuthenticateStopsWhenERPRejectsOTPRequest(t *testing.T) {
	secrets := t.TempDir()
	credentials, _ := json.Marshal(map[string]any{
		"roll_number": "23XX00000", "password": "password", "answers": map[string]string{"Question?": "answer"},
	})
	if err := os.WriteFile(filepath.Join(secrets, "erpcreds.json"), credentials, 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/SSOAdministration/getSecurityQues.htm":
			fmt.Fprint(writer, "Question?")
		case "/SSOAdministration/getEmilOTP.htm":
			fmt.Fprint(writer, `{"msg":"Unable to send OTP. Make sure answare of the security question has been provided."}`)
		default:
			t.Errorf("unexpected request to %s", request.URL.Path)
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	provider := &countingOTP{}
	client, _ := New(server.URL, secrets)
	client.OTP = provider
	err := client.Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Unable to send OTP") {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if provider.prepareCalls != 1 || provider.waitCalls != 0 {
		t.Fatalf("OTP provider calls: Prepare=%d Wait=%d", provider.prepareCalls, provider.waitCalls)
	}
}

func TestAuthenticateAndReuseSession(t *testing.T) {
	secrets := t.TempDir()
	credentials, _ := json.Marshal(map[string]any{
		"roll_number": "23XX00000", "password": "password", "answers": map[string]string{"Question?": "answer"},
	})
	if err := os.WriteFile(filepath.Join(secrets, "erpcreds.json"), credentials, 0o600); err != nil {
		t.Fatal(err)
	}
	loginCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/IIT_ERP3/":
			cookie, err := request.Cookie("ssoToken")
			if err != nil || cookie.Value != "session-token" {
				http.Redirect(writer, request, "/SSOAdministration/login.htm", http.StatusFound)
				return
			}
			fmt.Fprint(writer, `<title>Welcome Test Student to ERP, IIT Kharagpur</title><script src="getModules.htm"></script>`)
		case "/SSOAdministration/getSecurityQues.htm":
			fmt.Fprint(writer, "Question?")
		case "/SSOAdministration/getEmilOTP.htm":
			fmt.Fprint(writer, `{"msg":"An OTP has been sent"}`)
		case "/SSOAdministration/auth.htm":
			loginCount++
			if request.FormValue("email_otp") != "123456" {
				t.Errorf("unexpected OTP")
			}
			fmt.Fprint(writer, `location.href="/IIT_ERP3/?ssoToken=session-token"`)
		case "/IIT_ERP3/showmenu.htm":
			fmt.Fprint(writer, `<form id="menuform" method="post" action="../TrainingPlacementSSO/TPStudent.jsp"><input name="ssoToken" value="session-token"></form>`)
		case "/TrainingPlacementSSO/TPStudent.jsp":
			fmt.Fprint(writer, `<a href="StudentForm.jsp">Profile</a><script>url = "cvGenerate.jsp"</script>`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, secrets)
	if err != nil {
		t.Fatal(err)
	}
	client.OTP = staticOTP("123456")
	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loginCount != 1 {
		t.Fatalf("login count = %d", loginCount)
	}

	reused, _ := New(server.URL, secrets)
	reused.OTP = staticOTP("unused")
	if err := reused.Authenticate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loginCount != 1 {
		t.Fatal("saved session was not reused")
	}

	fresh, _ := New(server.URL, secrets)
	fresh.OTP = staticOTP("123456")
	if err := fresh.AuthenticateFresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loginCount != 2 {
		t.Fatal("fresh login did not discard the saved session")
	}
}

func TestBrowserLoginURLReturnsFreshUnvalidatedToken(t *testing.T) {
	secrets := t.TempDir()
	credentials, _ := json.Marshal(map[string]any{
		"roll_number": "23XX00000", "password": "p&ssword", "answers": map[string]string{"Question?": "answer"},
	})
	if err := os.WriteFile(filepath.Join(secrets, "erpcreds.json"), credentials, 0o600); err != nil {
		t.Fatal(err)
	}
	loginCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/SSOAdministration/getSecurityQues.htm":
			fmt.Fprint(writer, "Question?")
		case "/SSOAdministration/getEmilOTP.htm":
			fmt.Fprint(writer, `{"msg":"An OTP has been sent"}`)
		case "/SSOAdministration/auth.htm":
			loginCount++
			if request.FormValue("email_otp") != "654321" {
				t.Errorf("unexpected OTP")
			}
			fmt.Fprint(writer, `location.href="/IIT_ERP3/?ssoToken=fresh-browser-token"`)
		case "/IIT_ERP3/", "/IIT_ERP3/showmenu.htm", "/TrainingPlacementSSO/TPStudent.jsp":
			t.Fatalf("BrowserLoginURL should not consume the browser handoff token with %s", request.URL.Path)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, secrets)
	if err != nil {
		t.Fatal(err)
	}
	client.OTP = staticOTP("654321")
	got, err := client.BrowserLoginURL(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := server.URL + "/IIT_ERP3/?ssoToken=fresh-browser-token"
	if got != want {
		t.Fatalf("BrowserLoginURL() = %q, want %q", got, want)
	}
	if loginCount != 1 {
		t.Fatalf("login count = %d, want 1", loginCount)
	}
	saved, err := os.ReadFile(filepath.Join(secrets, ".session"))
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != "ssoToken=fresh-browser-token\n" {
		t.Fatalf("saved session = %q", saved)
	}
}

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("temporary network failure")
}

func TestAuthenticatePreservesSessionOnNetworkFailure(t *testing.T) {
	secrets := t.TempDir()
	sessionPath := filepath.Join(secrets, ".session")
	if err := os.WriteFile(sessionPath, []byte("ssoToken=keep-me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, _ := New("https://erp.example", secrets)
	client.HTTP.Transport = errorTransport{}
	if err := client.Authenticate(context.Background()); err == nil {
		t.Fatal("network failure was accepted")
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatal("saved session was deleted after a network failure")
	}
}

func TestValidSessionBodyRejectsAccessDeniedPage(t *testing.T) {
	denied := []byte(`You may not have access to this page. Direct access to any URL is restricted.`)
	if validSessionBody(denied) {
		t.Fatal("access-denied page was accepted as a valid ERP session")
	}
	valid := []byte(`<title>Welcome Test Student to ERP, IIT Kharagpur</title><script>url = "getModules.htm"</script>`)
	if !validSessionBody(valid) {
		t.Fatal("logged-in ERP page was rejected")
	}
}

func TestAuthenticatedHomepageURL(t *testing.T) {
	secrets := t.TempDir()
	if err := os.WriteFile(filepath.Join(secrets, ".session"), []byte("ssoToken=token with spaces\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, _ := New("https://erp.example", secrets)
	got, err := client.AuthenticatedHomepageURL()
	if err != nil {
		t.Fatal(err)
	}
	want := "https://erp.example/IIT_ERP3/?ssoToken=token+with+spaces"
	if got != want {
		t.Fatalf("AuthenticatedHomepageURL() = %q, want %q", got, want)
	}
}

func TestAuthenticateReplacesAccessDeniedSavedSession(t *testing.T) {
	secrets := t.TempDir()
	credentials, _ := json.Marshal(map[string]any{
		"roll_number": "23XX00000", "password": "password", "answers": map[string]string{"Question?": "answer"},
	})
	_ = os.WriteFile(filepath.Join(secrets, "erpcreds.json"), credentials, 0o600)
	_ = os.WriteFile(filepath.Join(secrets, ".session"), []byte("ssoToken=stale-token\n"), 0o600)
	loginCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/IIT_ERP3/":
			cookie, _ := request.Cookie("ssoToken")
			if cookie == nil || cookie.Value != "new-token" {
				fmt.Fprint(writer, "You may not have access to this page. Direct access to any URL is restricted.")
				return
			}
			fmt.Fprint(writer, `<title>Welcome to ERP</title><script src="getModules.htm"></script>`)
		case "/SSOAdministration/getSecurityQues.htm":
			fmt.Fprint(writer, "Question?")
		case "/SSOAdministration/getEmilOTP.htm":
			fmt.Fprint(writer, `{"msg":"An OTP has been sent"}`)
		case "/SSOAdministration/auth.htm":
			loginCount++
			fmt.Fprint(writer, `ssoToken=new-token`)
		case "/IIT_ERP3/showmenu.htm":
			fmt.Fprint(writer, `<form id="menuform" method="post" action="../TrainingPlacementSSO/TPStudent.jsp"><input name="ssoToken" value="new-token"></form>`)
		case "/TrainingPlacementSSO/TPStudent.jsp":
			fmt.Fprint(writer, `<a href="StudentForm.jsp">Profile</a><script>url = "cvGenerate.jsp"</script>`)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, secrets)
	client.OTP = staticOTP("123456")
	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loginCount != 1 {
		t.Fatalf("expected stale session replacement, login count = %d", loginCount)
	}
}

func TestAuthenticateReplacesSavedSessionRejectedByTrainingPlacement(t *testing.T) {
	secrets := t.TempDir()
	credentials, _ := json.Marshal(map[string]any{
		"roll_number": "23XX00000", "password": "password", "answers": map[string]string{"Question?": "answer"},
	})
	_ = os.WriteFile(filepath.Join(secrets, "erpcreds.json"), credentials, 0o600)
	_ = os.WriteFile(filepath.Join(secrets, ".session"), []byte("ssoToken=stale-token\n"), 0o600)
	loginCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/IIT_ERP3/":
			fmt.Fprint(writer, `<title>Welcome to ERP</title><script src="getModules.htm"></script>`)
		case "/IIT_ERP3/showmenu.htm":
			cookie, _ := request.Cookie("ssoToken")
			if cookie == nil || cookie.Value != "new-token" {
				fmt.Fprint(writer, "You may not have access to this page. Direct access to any URL is restricted.")
				return
			}
			fmt.Fprint(writer, `<form id="menuform" method="post" action="../TrainingPlacementSSO/TPStudent.jsp"><input name="ssoToken" value="new-token"></form>`)
		case "/TrainingPlacementSSO/TPStudent.jsp":
			fmt.Fprint(writer, `<a href="StudentForm.jsp">Profile</a><script>url = "cvGenerate.jsp"</script>`)
		case "/SSOAdministration/getSecurityQues.htm":
			fmt.Fprint(writer, "Question?")
		case "/SSOAdministration/getEmilOTP.htm":
			fmt.Fprint(writer, `{"msg":"An OTP has been sent"}`)
		case "/SSOAdministration/auth.htm":
			loginCount++
			fmt.Fprint(writer, `ssoToken=new-token`)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, secrets)
	client.OTP = staticOTP("123456")
	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loginCount != 1 {
		t.Fatalf("expected Training Placement rejection to force login, login count = %d", loginCount)
	}
}

func TestSyncAndDownload(t *testing.T) {
	values := map[string]string{
		"standard7": "Experience",
		"subject7":  `<ul><li><span style="font-size: 9px;">&nbsp;Added fast math.</span></li></ul>`,
	}
	formHTML := func() string {
		return fmt.Sprintf(`<!doctype html><form id="from2_stu" action="SFA.jsp"><input name="mode"><input name="standard7" value="%s"><textarea name="subject7">%s</textarea></form>`, values["standard7"], values["subject7"])
	}
	initialized := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/IIT_ERP3/showmenu.htm":
			if request.FormValue("module_id") != cdcModuleID || request.FormValue("menu_id") != placementMenuID {
				t.Error("incorrect CDC navigation fields")
			}
			fmt.Fprint(writer, `<form id="menuform" method="post" action="../TrainingPlacementSSO/TPStudent.jsp"><input name="module_id" value="26"><input name="menu_id" value="11"><input name="ssoToken" value="test-token"></form>`)
		case "/TrainingPlacementSSO/TPStudent.jsp":
			if request.FormValue("ssoToken") != "test-token" {
				t.Error("navigation form values were not forwarded")
			}
			initialized = true
			fmt.Fprint(writer, `<title>Career Development Centre</title><a href="StudentForm.jsp">Profile</a><script>url = "cvGenerate.jsp"</script>`)
		case "/TrainingPlacementSSO/StudentForm.jsp":
			if !initialized {
				http.Error(writer, "Training Placement session was not initialized", http.StatusForbidden)
				return
			}
			fmt.Fprint(writer, formHTML())
		case "/TrainingPlacementSSO/SFA.jsp":
			if request.FormValue("mode") != "checkdata" {
				t.Error("mode was not checkdata")
			}
			values["standard7"] = request.FormValue("standard7")
			values["subject7"] = request.FormValue("subject7")
			fmt.Fprint(writer, "saved")
		case "/TrainingPlacementSSO/cvGenerate.jsp":
			writer.Header().Set("Content-Type", "application/pdf")
			data := append([]byte("%PDF-1.4\n"), make([]byte, 80)...)
			data = append(data, []byte("\n%%EOF\n")...)
			_, _ = writer.Write(data)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, t.TempDir())
	wanted := map[string]string{
		"standard7": "Experience",
		"subject7":  `<ul><li><span style="font-size: 9px;">&nbsp;Added fast math.</span></li></ul>`,
	}
	if err := client.SyncResume(context.Background(), wanted); err != nil {
		t.Fatal(err)
	}
	pdf, err := client.DownloadPDF(context.Background(), 1)
	if err != nil || !strings.HasPrefix(string(pdf), "%PDF-") {
		t.Fatalf("DownloadPDF() error=%v", err)
	}
}

func TestResolveTrustedActionRejectsAnotherHost(t *testing.T) {
	_, err := resolveTrustedAction("https://erp.example/IIT_ERP3/showmenu.htm", "https://attacker.example/steal", "https://erp.example")
	if err == nil {
		t.Fatal("untrusted navigation action was accepted")
	}
}
