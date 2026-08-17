package erp

import (
	"encoding/base64"
	"testing"

	gmail "google.golang.org/api/gmail/v1"
)

func TestGmailBodyDecodesCommonEncodings(t *testing.T) {
	for name, encoded := range map[string]string{
		"raw URL": base64.RawURLEncoding.EncodeToString([]byte("OTP 123456")),
		"URL":     base64.URLEncoding.EncodeToString([]byte("OTP 123456")),
	} {
		t.Run(name, func(t *testing.T) {
			body := gmailBody(&gmail.MessagePart{Body: &gmail.MessagePartBody{Data: encoded}})
			if otpRE.FindString(body) != "123456" {
				t.Fatalf("decoded body %q", body)
			}
		})
	}
}
