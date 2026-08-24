package erp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cvpp/internal/pdfutil"
	"cvpp/internal/progress"
)

const DefaultBaseURL = "https://erp.iitkgp.ac.in"

var tokenRE = regexp.MustCompile(`ssoToken=[A-Za-z0-9._-]+`)

// ErrSessionRejected marks responses where ERP explicitly sent the client back
// to authentication or denied access. Callers can use this to decide whether a
// saved login should be replaced without guessing from error text.
var ErrSessionRejected = errors.New("ERP session rejected")

var deniedPageMarkers = []string{
	"you may not have access to this page",
	"direct access to any url is restricted",
	"/ssoadministration/login",
	"/ssoadministration/auth",
}

type Client struct {
	HTTP                   *http.Client
	BaseURL                string
	SecretsDir             string
	OTP                    OTPProvider
	SecurityAnswers        SecurityAnswerProvider
	trainingPlacementReady bool
	sessionToken           string
	Phase                  func(string)
}

type Credentials struct {
	RollNumber string            `json:"roll_number"`
	Password   string            `json:"password"`
	Answers    map[string]string `json:"answers"`
}

type credentials = Credentials

type OTPProvider interface {
	Prepare(context.Context) error
	Wait(context.Context) (string, error)
}

type SecurityAnswerProvider interface {
	Wait(context.Context, string) (string, error)
}

func New(baseURL, secretsDir string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	client := &Client{
		HTTP: &http.Client{
			Jar:     jar,
			Timeout: 45 * time.Second,
		},
		BaseURL:    strings.TrimRight(baseURL, "/"),
		SecretsDir: secretsDir,
	}
	client.OTP = NewDefaultOTP(secretsDir)
	return client, nil
}

func (c *Client) Authenticate(ctx context.Context) error {
	return c.authenticate(ctx, false)
}

func (c *Client) AuthenticateFresh(ctx context.Context) error {
	return c.authenticate(ctx, true)
}

func (c *Client) DiscardSavedSession() error {
	if err := os.Remove(filepath.Join(c.SecretsDir, ".session")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove saved ERP session: %w", err)
	}
	return nil
}

// RestoreSavedSession rebuilds the in-memory ERP cookie jar from the locally
// saved SSO token and validates the associated application sessions.
func (c *Client) RestoreSavedSession(ctx context.Context) error {
	token, err := c.readSession()
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("saved ERP session is empty")
	}
	c.installCookie(token)
	return c.validateSession(ctx, token)
}

// KeepAlive refreshes an already-restored ERP session using the same endpoint
// and request shape as the ERP web application. It never authenticates or
// reads credentials when the session has expired.
func (c *Client) KeepAlive(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/IIT_ERP3/keepAlive.htm", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Referer", c.BaseURL+"/IIT_ERP3/showmenu.htm")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return safeHTTPError(err)
	}
	body, readErr := readLimited(response.Body, 256<<10)
	response.Body.Close()
	if readErr != nil {
		return readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || isAuthURL(response.Request.URL) || isDeniedPage(body) {
		return fmt.Errorf("%w: ERP keep-alive was rejected", ErrSessionRejected)
	}
	return nil
}

// Logout closes the authenticated IIT ERP session represented by this
// client's cookie jar. The saved SSO token is removed only after ERP accepts
// the logout request, so a transient network failure can be retried later.
func (c *Client) Logout(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/IIT_ERP3/logout.htm", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	request.Header.Set("Accept-Language", "en-US,en;q=0.9,en-GB;q=0.8")
	request.Header.Set("Referer", c.BaseURL+"/IIT_ERP3/home.htm")
	request.Header.Set("Sec-Fetch-Dest", "document")
	request.Header.Set("Sec-Fetch-Mode", "navigate")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Sec-Fetch-User", "?1")
	request.Header.Set("Upgrade-Insecure-Requests", "1")
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36")

	response, err := c.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("ERP logout request: %w", safeHTTPError(err))
	}
	_, readErr := readLimited(response.Body, 2<<20)
	response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read ERP logout response: %w", readErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("ERP logout returned %s", response.Status)
	}
	if err := c.DiscardSavedSession(); err != nil {
		return fmt.Errorf("ERP logout succeeded but local session cleanup failed: %w", err)
	}
	c.trainingPlacementReady = false
	c.sessionToken = ""
	progress.Logf("ERP session: logout succeeded")
	return nil
}

// LoadCredentials reads the local credential file for setup and tests. It is
// intentionally not used by any GET handler, so secrets cannot be reflected
// through the browser API.
func LoadCredentials(path string) (Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("parse ERP credentials: %w", err)
	}
	if strings.TrimSpace(creds.RollNumber) == "" || strings.TrimSpace(creds.Password) == "" || len(creds.Answers) == 0 {
		return Credentials{}, errors.New("ERP credentials are incomplete")
	}
	return creds, nil
}

func SaveCredentials(path string, creds Credentials) error {
	creds.RollNumber = strings.TrimSpace(creds.RollNumber)
	if creds.RollNumber == "" || strings.TrimSpace(creds.Password) == "" || len(creds.Answers) == 0 {
		return errors.New("ERP credentials are incomplete")
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0o600)
}

func (c *Client) FetchSecurityQuestion(ctx context.Context, rollNumber string) (string, error) {
	rollNumber = strings.TrimSpace(rollNumber)
	if rollNumber == "" {
		return "", errors.New("ERP roll number is required")
	}
	question, err := c.postText(ctx, "/SSOAdministration/getSecurityQues.htm", url.Values{"user_id": {rollNumber}})
	if err != nil {
		return "", fmt.Errorf("fetch ERP security question: %w", err)
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return "", errors.New("ERP returned an empty security question")
	}
	return question, nil
}

func (c *Client) authenticate(ctx context.Context, fresh bool) error {
	if fresh {
		progress.Logf("ERP auth: discarding the saved session")
		if err := c.DiscardSavedSession(); err != nil {
			return err
		}
	}
	if token, err := c.readSession(); err == nil && token != "" {
		progress.Logf("ERP auth: validating the saved session")
		c.installCookie(token)
		validationErr := c.validateSession(ctx, token)
		if validationErr == nil {
			progress.Logf("ERP auth: saved session is valid")
			return nil
		}
		if !errors.Is(validationErr, ErrSessionRejected) {
			return fmt.Errorf("validate saved ERP session: %w", validationErr)
		}
		progress.Logf("ERP auth: saved session is invalid; starting a new login")
		_ = os.Remove(filepath.Join(c.SecretsDir, ".session"))
	}

	challenge, err := c.loginChallenge(ctx, "ERP auth")
	if err != nil {
		return err
	}
	otp, err := c.requestAndWaitOTP(ctx, "ERP auth", challenge)
	if err != nil {
		return err
	}

	progress.Logf("ERP auth: submitting login")
	response, err := c.post(ctx, "/SSOAdministration/auth.htm", url.Values{
		"user_id": {challenge.Credentials.RollNumber}, "password": {challenge.Credentials.Password}, "answer": {challenge.Answer},
		"requestedUrl": {c.BaseURL + "/IIT_ERP3/"}, "email_otp": {otp},
	})
	if err != nil {
		return fmt.Errorf("ERP login: %w", err)
	}
	body, err := readLimited(response.Body, 2<<20)
	response.Body.Close()
	if err != nil {
		return err
	}
	token := c.tokenFromResponse(response, body)
	if token != "" {
		c.installCookie(token)
	}
	progress.Logf("ERP auth: validating the new login")
	if err := c.validateLoginResponse(ctx, response, body, token); err != nil {
		return fmt.Errorf("new ERP session is invalid: %w", err)
	}
	if token == "" {
		token = c.sessionToken
	}
	if token == "" {
		token = c.tokenFromJar()
	}
	if token == "" {
		return fmt.Errorf("ERP login succeeded, but ERP did not expose a reusable session token")
	}
	if err := atomicWrite(filepath.Join(c.SecretsDir, ".session"), []byte("ssoToken="+token+"\n"), 0o600); err != nil {
		return fmt.Errorf("save ERP session: %w", err)
	}
	progress.Logf("ERP auth: login complete and session saved")
	return nil
}

type loginChallenge struct {
	Credentials *credentials
	Question    string
	Answer      string
	NewAnswer   bool
}

func (c *Client) loginChallenge(ctx context.Context, label string) (*loginChallenge, error) {
	progress.Logf("%s: loading credentials", label)
	creds, err := c.readCredentials()
	if err != nil {
		return nil, err
	}
	progress.Logf("%s: fetching the security question", label)
	question, err := c.postText(ctx, "/SSOAdministration/getSecurityQues.htm", url.Values{"user_id": {creds.RollNumber}})
	if err != nil {
		return nil, fmt.Errorf("fetch ERP security question: %w", err)
	}
	question = strings.TrimSpace(question)
	answer, ok := answerForQuestion(creds.Answers, question)
	if !ok {
		if c.SecurityAnswers == nil {
			return nil, fmt.Errorf("erpcreds.json has no answer for the returned security question %q", question)
		}
		progress.Logf("%s: waiting for the answer to a new security question", label)
		if c.Phase != nil {
			c.Phase("security-answer-required")
		}
		answer, err = c.SecurityAnswers.Wait(ctx, question)
		if err != nil {
			return nil, fmt.Errorf("get ERP security answer: %w", err)
		}
	}
	if strings.TrimSpace(answer) == "" {
		return nil, fmt.Errorf("erpcreds.json has an empty answer for the returned security question %q", question)
	}
	return &loginChallenge{Credentials: creds, Question: question, Answer: answer, NewAnswer: !ok}, nil
}

func answerForQuestion(answers map[string]string, question string) (string, bool) {
	if answer, ok := answers[question]; ok {
		return answer, true
	}
	normalizedQuestion := normalizeSecurityQuestion(question)
	for savedQuestion, answer := range answers {
		if normalizeSecurityQuestion(savedQuestion) == normalizedQuestion {
			return answer, true
		}
	}
	return "", false
}

func normalizeSecurityQuestion(question string) string {
	return strings.ToLower(strings.Join(strings.Fields(question), " "))
}

func (c *Client) requestAndWaitOTP(ctx context.Context, label string, challenge *loginChallenge) (string, error) {
	automatic := isAutomaticOTP(c.OTP)
	if automatic {
		progress.Logf("%s: preparing automatic OTP retrieval", label)
	} else {
		progress.Logf("%s: using manual OTP entry", label)
	}
	if err := c.OTP.Prepare(ctx); err != nil {
		if !automatic {
			return "", err
		}
		progress.Logf("%s: automatic OTP preparation failed; manual input will be used (%v)", label, err)
		c.OTP = NewManualOTP()
		automatic = false
		if err := c.OTP.Prepare(ctx); err != nil {
			return "", err
		}
	}
	progress.Logf("%s: requesting an OTP", label)
	otpBody, err := c.postText(ctx, "/SSOAdministration/getEmilOTP.htm", url.Values{
		"typeee": {"SI"}, "user_id": {challenge.Credentials.RollNumber}, "password": {challenge.Credentials.Password}, "answer": {challenge.Answer},
	})
	if err != nil {
		return "", fmt.Errorf("request ERP OTP: %w", err)
	}
	var otpResponse struct {
		Message string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(otpBody), &otpResponse); err != nil {
		return "", fmt.Errorf("ERP returned an invalid OTP response")
	}
	if !otpRequestAccepted(otpResponse.Message) {
		return "", fmt.Errorf("ERP rejected the OTP request: %s", safeMessage(otpResponse.Message))
	}
	if challenge.NewAnswer {
		challenge.Credentials.Answers[challenge.Question] = challenge.Answer
		if err := SaveCredentials(filepath.Join(c.SecretsDir, "erpcreds.json"), *challenge.Credentials); err != nil {
			return "", fmt.Errorf("save ERP security answer: %w", err)
		}
		challenge.NewAnswer = false
		progress.Logf("%s: saved the new security answer locally", label)
	}
	progress.Logf("%s: OTP request accepted by ERP", label)
	if c.Phase != nil {
		c.Phase("otp-required")
	}
	if automatic {
		progress.Logf("%s: waiting for the OTP email", label)
	}
	otp, err := c.OTP.Wait(ctx)
	if err != nil {
		if automatic {
			progress.Logf("%s: automatic OTP retrieval failed; enter it manually (%v)", label, err)
			otp, err = NewManualOTP().Wait(ctx)
			if err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}
	return otp, nil
}

func (c *Client) Get(ctx context.Context, path string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, safeHTTPError(err)
	}
	return response, nil
}

func (c *Client) PostForm(ctx context.Context, path string, values url.Values) (*http.Response, error) {
	return c.post(ctx, path, values)
}

func (c *Client) AuthenticatedHomepageURL() (string, error) {
	token, err := c.readSession()
	if err != nil {
		return "", fmt.Errorf("read saved ERP session: %w", err)
	}
	if token == "" {
		return "", fmt.Errorf("saved ERP session is empty")
	}
	return c.BaseURL + "/IIT_ERP3/?ssoToken=" + url.QueryEscape(token), nil
}

func (c *Client) DownloadPDF(ctx context.Context, variant int) ([]byte, error) {
	if variant < 1 || variant > 3 {
		return nil, fmt.Errorf("CV number must be 1, 2, or 3")
	}
	progress.Logf("ERP PDF: opening the resume form")
	if _, err := c.FetchResumeForm(ctx); err != nil {
		return nil, fmt.Errorf("initialize Training Placement session: %w", err)
	}
	pdfURL := c.BaseURL + fmt.Sprintf("/TrainingPlacementSSO/cvGenerate.jsp?resume_no=%d", variant)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pdfURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/pdf,application/octet-stream;q=0.9,*/*;q=0.8")
	request.Header.Set("Accept-Language", "en-US,en;q=0.9")
	request.Header.Set("Referer", c.BaseURL+"/TrainingPlacementSSO/StudentForm.jsp")
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0")
	progress.Logf("ERP PDF: requesting CV%d", variant)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := readLimited(response.Body, 25<<20)
	if err != nil {
		return nil, err
	}
	progress.Logf("ERP PDF: response %s, %d bytes, content-type %q, content-length %q, final URL %s",
		response.Status, len(data), response.Header.Get("Content-Type"), response.Header.Get("Content-Length"), redactedURL(response.Request.URL))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ERP PDF request returned %s", response.Status)
	}
	if isAuthURL(response.Request.URL) {
		return nil, fmt.Errorf("%w: ERP redirected the PDF request to login", ErrSessionRejected)
	}
	if isDeniedPage(data) {
		return nil, fmt.Errorf("%w: ERP rejected the CV%d PDF request", ErrSessionRejected, variant)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("ERP CV generator returned an empty PDF body for CV%d; the resume form was saved, but the PDF endpoint did not render a document", variant)
	}
	if err := pdfutil.Validate(data); err != nil {
		excerpt := safeMessage(string(data))
		if excerpt != "" {
			return nil, fmt.Errorf("ERP returned an invalid PDF (%s, %q): %w", response.Header.Get("Content-Type"), excerpt, err)
		}
		return nil, fmt.Errorf("ERP returned an invalid PDF (%s, first bytes %x): %w", response.Header.Get("Content-Type"), data, err)
	}
	progress.Logf("ERP PDF: CV%d passed PDF validation", variant)
	return data, nil
}

func (c *Client) validateSession(ctx context.Context, token string) error {
	response, err := c.Get(ctx, "/IIT_ERP3/?ssoToken="+url.QueryEscape(token))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || isAuthURL(response.Request.URL) {
		return fmt.Errorf("%w: ERP returned %s", ErrSessionRejected, response.Status)
	}
	body, err := readLimited(response.Body, 2<<20)
	if err != nil {
		return err
	}
	if !validSessionBody(body) {
		return fmt.Errorf("%w: ERP returned a login or access-denied page", ErrSessionRejected)
	}
	if err := c.validateTrainingPlacementAccess(ctx); err != nil {
		return err
	}
	return nil
}

// validateLoginResponse avoids replaying a one-time SSO handoff token when the
// authentication POST already followed ERP's redirect to the logged-in home
// page. Replaying that token can make a successful new login look expired.
func (c *Client) validateLoginResponse(ctx context.Context, response *http.Response, body []byte, token string) error {
	if response != nil && response.Request != nil && isERPHomeURL(response.Request.URL) &&
		response.StatusCode >= 200 && response.StatusCode < 300 && validSessionBody(body) {
		progress.Logf("ERP auth: login redirect already established the session")
		return c.validateTrainingPlacementAccess(ctx)
	}
	if token == "" {
		response, err := c.Get(ctx, "/IIT_ERP3/")
		if err != nil {
			return err
		}
		defer response.Body.Close()
		currentBody, err := readLimited(response.Body, 2<<20)
		if err != nil {
			return err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 || isAuthURL(response.Request.URL) || !validSessionBody(currentBody) {
			return fmt.Errorf("%w: ERP did not establish an authenticated application session", ErrSessionRejected)
		}
		return c.validateTrainingPlacementAccess(ctx)
	}
	return c.validateSession(ctx, token)
}

func validSessionBody(body []byte) bool {
	if isDeniedPage(body) {
		return false
	}
	lower := strings.ToLower(string(body))
	validMarkers := []string{"getmodules.htm", "showmenu.htm", "logout", "welcome"}
	for _, marker := range validMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isDeniedPage(body []byte) bool {
	lower := strings.ToLower(string(body))
	for _, marker := range deniedPageMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isERPHomeURL(value *url.URL) bool {
	if value == nil {
		return false
	}
	path := strings.ToLower(strings.TrimSuffix(value.Path, "/"))
	return path == "/iit_erp3" || strings.HasPrefix(path, "/iit_erp3/")
}

func (c *Client) postText(ctx context.Context, path string, values url.Values) (string, error) {
	response, err := c.post(ctx, path, values)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := readLimited(response.Body, 2<<20)
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("server returned %s", response.Status)
	}
	return string(body), nil
}

func (c *Client) post(ctx context.Context, path string, values url.Values) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, safeHTTPError(err)
	}
	return response, nil
}

func (c *Client) readCredentials() (*credentials, error) {
	creds, err := LoadCredentials(filepath.Join(c.SecretsDir, "erpcreds.json"))
	if err != nil {
		return nil, fmt.Errorf("read ERP credentials: %w", err)
	}
	return &creds, nil
}

func (c *Client) readSession() (string, error) {
	data, err := os.ReadFile(filepath.Join(c.SecretsDir, ".session"))
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(data)), "ssoToken="), nil
}

func (c *Client) installCookie(token string) {
	root, _ := url.Parse(c.BaseURL + "/")
	c.HTTP.Jar.SetCookies(root, []*http.Cookie{{Name: "ssoToken", Value: token, Path: "/", Secure: strings.HasPrefix(c.BaseURL, "https://")}})
	c.sessionToken = token
}

func (c *Client) tokenFromResponse(response *http.Response, body []byte) string {
	for current, redirects := response, 0; current != nil && redirects < 12; redirects++ {
		if current.Request != nil && current.Request.URL != nil {
			if token := current.Request.URL.Query().Get("ssoToken"); token != "" {
				return token
			}
		}
		for _, cookie := range current.Cookies() {
			if cookie.Name == "ssoToken" && cookie.Value != "" {
				return cookie.Value
			}
		}
		if location := current.Header.Get("Location"); location != "" {
			if parsed, err := url.Parse(location); err == nil {
				if token := parsed.Query().Get("ssoToken"); token != "" {
					return token
				}
			}
			if match := tokenRE.FindString(location); match != "" {
				return strings.TrimPrefix(match, "ssoToken=")
			}
		}
		if current.Request == nil {
			break
		}
		current = current.Request.Response
	}
	if match := tokenRE.Find(body); len(match) > 0 {
		return strings.TrimPrefix(string(match), "ssoToken=")
	}
	return c.tokenFromJar()
}

func (c *Client) tokenFromJar() string {
	for _, path := range []string{"/", "/IIT_ERP3/", "/SSOAdministration/"} {
		location, err := url.Parse(c.BaseURL + path)
		if err != nil {
			continue
		}
		for _, cookie := range c.HTTP.Jar.Cookies(location) {
			if cookie.Name == "ssoToken" && cookie.Value != "" {
				return cookie.Value
			}
		}
	}
	return ""
}

func isAuthURL(value *url.URL) bool {
	return value != nil && strings.Contains(strings.ToLower(value.Path), "/ssoadministration/")
}

func redactedURL(value *url.URL) string {
	if value == nil {
		return ""
	}
	copy := *value
	query := copy.Query()
	for _, name := range []string{"ssoToken", "token", "session", "JSESSIONID"} {
		if query.Has(name) {
			query.Set(name, "REDACTED")
		}
	}
	copy.RawQuery = query.Encode()
	return copy.String()
}

func safeHTTPError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return &url.Error{Op: urlErr.Op, URL: redactURLString(urlErr.URL), Err: urlErr.Err}
	}
	return err
}

func redactURLString(value string) string {
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil {
		return redactedURL(parsed)
	}
	return tokenRE.ReplaceAllString(value, "ssoToken=REDACTED")
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	var output bytes.Buffer
	count, err := io.CopyN(&output, reader, limit+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if count > limit {
		return nil, fmt.Errorf("response exceeded %d bytes", limit)
	}
	return output.Bytes(), nil
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

func safeMessage(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 180 {
		value = value[:180] + "..."
	}
	return value
}

func otpRequestAccepted(message string) bool {
	message = strings.ToLower(strings.Join(strings.Fields(message), " "))
	return strings.Contains(message, "otp") && strings.Contains(message, "has been sent")
}
