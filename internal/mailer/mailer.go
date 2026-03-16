// Package mailer sends transactional emails via the Resend REST API.
// It deliberately avoids an SDK dependency — the Resend API surface used here
// is stable and tiny (POST /emails with JSON body + Authorization header).
package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const resendSendURL = "https://api.resend.com/emails"

// Mailer sends transactional emails.
type Mailer struct {
	apiKey     string
	from       string
	httpClient *http.Client
}

// New constructs a Mailer. apiKey is the Resend API key; from is the sender
// address (e.g. "LLMRates <noreply@llmrates.live>").
func New(apiKey, from string) *Mailer {
	return &Mailer{
		apiKey: apiKey,
		from:   from,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// sendPayload is the JSON body sent to POST /emails.
type sendPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

// SendMagicLink emails a one-time verification link to the given address.
// verifyURL is the full URL including the raw token query parameter.
func (m *Mailer) SendMagicLink(ctx context.Context, toEmail, verifyURL string) error {
	html := magicLinkHTML(verifyURL)
	payload := sendPayload{
		From:    m.from,
		To:      []string{toEmail},
		Subject: "Your LLMRates sign-in link",
		HTML:    html,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mailer: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendSendURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mailer: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.apiKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mailer: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody struct {
			Name    string `json:"name"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return fmt.Errorf("mailer: resend API %d: %s — %s", resp.StatusCode, errBody.Name, errBody.Message)
	}
	return nil
}

// magicLinkHTML returns the email body for a magic-link request.
func magicLinkHTML(verifyURL string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="font-family:system-ui,sans-serif;background:#0a0a0a;color:#e5e5e5;padding:40px 24px;margin:0">
  <table width="100%" cellpadding="0" cellspacing="0" style="max-width:480px;margin:0 auto">
    <tr><td>
      <p style="font-size:13px;color:#888;margin:0 0 32px">LLMRates</p>
      <h1 style="font-size:22px;font-weight:600;margin:0 0 16px;color:#f5f5f5">Your sign-in link</h1>
      <p style="font-size:15px;color:#aaa;margin:0 0 32px;line-height:1.6">
        Click the button below to verify your email and get your free API key.
        This link expires shortly and can only be used once.
      </p>
      <a href="` + verifyURL + `"
         style="display:inline-block;background:#22c55e;color:#0a0a0a;font-size:14px;
                font-weight:600;padding:12px 28px;border-radius:6px;text-decoration:none">
        Verify email →
      </a>
      <p style="font-size:12px;color:#555;margin:32px 0 0;line-height:1.5">
        If you didn't request this, you can safely ignore this email.<br>
        This link will expire automatically.
      </p>
    </td></tr>
  </table>
</body>
</html>`
}
