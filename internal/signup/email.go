package signup

import (
	"context"
	"fmt"

	resend "github.com/resend/resend-go/v2"
)

// Mailer abstracts magic-link email delivery for testability.
type Mailer interface {
	SendMagicLink(ctx context.Context, to, magicLinkURL string, ttlMinutes int) error
}

// resendMailer delivers magic-link emails via the Resend API.
type resendMailer struct {
	client *resend.Client
	from   string
}

// NewResendMailer creates a Mailer backed by the Resend API.
func NewResendMailer(apiKey, from string) Mailer {
	return &resendMailer{
		client: resend.NewClient(apiKey),
		from:   from,
	}
}

// SendMagicLink sends a magic-link email to `to`. ttlMinutes is rendered into
// the email body so the stated expiry always matches the configured token TTL.
func (m *resendMailer) SendMagicLink(_ context.Context, to, magicLinkURL string, ttlMinutes int) error {
	params := &resend.SendEmailRequest{
		From:    m.from,
		To:      []string{to},
		Subject: "Your LLMRates.live API key link",
		Html: fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family:sans-serif;background:#0a0a0a;color:#e0e0e0;padding:32px">
  <h2 style="color:#5dcaa5">LLMRates.live</h2>
  <p>Click the link below to verify your email and receive your free API key.</p>
  <p>
    <a href="%s" style="display:inline-block;padding:12px 24px;background:#5dcaa5;color:#0a0a0a;
       border-radius:4px;text-decoration:none;font-weight:600">Get my API key</a>
  </p>
  <p style="font-size:12px;color:#666">This link expires in %d minutes and can only be used once.</p>
  <p style="font-size:12px;color:#666">If you didn't request this, you can safely ignore this email.</p>
</body>
</html>`, magicLinkURL, ttlMinutes),
		Text: fmt.Sprintf(
			"LLMRates.live — verify your email\n\nOpen this link to get your free API key:\n%s\n\n"+
				"The link expires in %d minutes and can only be used once.",
			magicLinkURL, ttlMinutes,
		),
	}
	_, err := m.client.Emails.Send(params)
	return err
}
