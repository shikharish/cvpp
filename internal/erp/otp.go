package erp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

const gmailQuery = "from:erpkgp@adm.iitkgp.ac.in subject:otp newer_than:1d"

var otpRE = regexp.MustCompile(`\b[0-9]{4,8}\b`)

type automaticOTP struct {
	secretsDir string
	service    *gmail.Service
	baseline   string
}

func NewDefaultOTP(secretsDir string) OTPProvider {
	if hasFile(filepath.Join(secretsDir, "client_secret.json")) && hasFile(filepath.Join(secretsDir, ".token")) {
		return NewAutomaticOTP(secretsDir)
	}
	return NewManualOTP()
}

func NewAutomaticOTP(secretsDir string) OTPProvider {
	return &automaticOTP{secretsDir: secretsDir}
}

func isAutomaticOTP(provider OTPProvider) bool {
	_, ok := provider.(*automaticOTP)
	return ok
}

func hasFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (p *automaticOTP) Prepare(ctx context.Context) error {
	secretData, err := os.ReadFile(filepath.Join(p.secretsDir, "client_secret.json"))
	if err != nil {
		return err
	}
	var secret struct {
		Installed struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(secretData, &secret); err != nil {
		return err
	}
	tokenData, err := os.ReadFile(filepath.Join(p.secretsDir, ".token"))
	if err != nil {
		return err
	}
	var token oauth2.Token
	if err := json.Unmarshal(tokenData, &token); err != nil {
		return err
	}
	config := oauth2.Config{
		ClientID: secret.Installed.ClientID, ClientSecret: secret.Installed.ClientSecret,
		Scopes: []string{gmail.GmailReadonlyScope}, Endpoint: google.Endpoint,
	}
	service, err := gmail.NewService(ctx, option.WithTokenSource(config.TokenSource(ctx, &token)))
	if err != nil {
		return err
	}
	p.service = service
	p.baseline, err = p.latestMessageID()
	return err
}

func (p *automaticOTP) Wait(ctx context.Context) (string, error) {
	if p.service == nil {
		return "", fmt.Errorf("Gmail OTP provider was not prepared")
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(2 * time.Minute)
	defer timeout.Stop()
	for {
		id, err := p.latestMessageID()
		if err != nil {
			return "", err
		}
		if id != "" && id != p.baseline {
			message, err := p.service.Users.Messages.Get("me", id).Format("full").Do()
			if err != nil {
				return "", err
			}
			body := message.Snippet + " " + gmailBody(message.Payload)
			if message.Payload != nil {
				for _, header := range message.Payload.Headers {
					body += " " + header.Value
				}
			}
			if otp := otpRE.FindString(body); otp != "" {
				return otp, nil
			}
			return "", fmt.Errorf("new ERP email did not contain an OTP")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout.C:
			return "", fmt.Errorf("timed out waiting for the ERP OTP email")
		case <-ticker.C:
		}
	}
}

func (p *automaticOTP) latestMessageID() (string, error) {
	results, err := p.service.Users.Messages.List("me").Q(gmailQuery).MaxResults(1).Do()
	if err != nil {
		return "", err
	}
	if len(results.Messages) == 0 {
		return "", nil
	}
	return results.Messages[0].Id, nil
}

func gmailBody(part *gmail.MessagePart) string {
	if part == nil {
		return ""
	}
	var output strings.Builder
	if part.Body != nil && part.Body.Data != "" {
		if decoded, err := decodeBase64(part.Body.Data); err == nil {
			output.Write(decoded)
		}
	}
	for _, child := range part.Parts {
		output.WriteString(gmailBody(child))
	}
	return output.String()
}

func decodeBase64(value string) ([]byte, error) {
	encodings := []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding}
	var last error
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
		last = err
	}
	return nil, last
}

type manualOTP struct{}

func NewManualOTP() OTPProvider                 { return manualOTP{} }
func (manualOTP) Prepare(context.Context) error { return nil }
func (manualOTP) Wait(ctx context.Context) (string, error) {
	fmt.Print("Enter ERP OTP: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(line)
	if !otpRE.MatchString(value) {
		return "", fmt.Errorf("invalid OTP")
	}
	return value, nil
}
