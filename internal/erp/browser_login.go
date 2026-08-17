package erp

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	"cvpp/internal/progress"
)

func (c *Client) BrowserLoginURL(ctx context.Context) (string, error) {
	challenge, err := c.loginChallenge(ctx, "ERP browser auth")
	if err != nil {
		return "", err
	}
	otp, err := c.requestAndWaitOTP(ctx, "ERP browser auth", challenge)
	if err != nil {
		return "", err
	}
	progress.Logf("ERP browser auth: submitting login for a fresh browser handoff token")
	response, err := c.post(ctx, "/SSOAdministration/auth.htm", url.Values{
		"user_id": {challenge.Credentials.RollNumber}, "password": {challenge.Credentials.Password}, "answer": {challenge.Answer},
		"requestedUrl": {c.BaseURL + "/IIT_ERP3/"}, "email_otp": {otp},
	})
	if err != nil {
		return "", fmt.Errorf("ERP browser login: %w", err)
	}
	body, err := readLimited(response.Body, 2<<20)
	response.Body.Close()
	if err != nil {
		return "", err
	}
	token := c.tokenFromResponse(response, body)
	if token == "" {
		return "", fmt.Errorf("ERP browser login did not return a session token")
	}
	c.installCookie(token)
	if err := atomicWrite(filepath.Join(c.SecretsDir, ".session"), []byte("ssoToken="+token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("save ERP session: %w", err)
	}
	progress.Logf("ERP browser auth: fresh handoff token saved for future CLI runs")
	return c.BaseURL + "/IIT_ERP3/?ssoToken=" + url.QueryEscape(token), nil
}
