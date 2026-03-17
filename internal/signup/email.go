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

// SendMagicLink sends a magic-link email to `to`. The ttlMinutes parameter is
// accepted for interface compatibility but the email copy uses a generic
// "expires shortly" phrase instead of an explicit duration.
func (m *resendMailer) SendMagicLink(ctx context.Context, to, magicLinkURL string, _ int) error {
	params := &resend.SendEmailRequest{
		From:    m.from,
		To:      []string{to},
		Subject: "Your LLMRates API key link",
		Html: fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#0a0a0a;color:#c8c4be;font-family:system-ui,-apple-system,sans-serif">
  <table role="presentation" cellpadding="0" cellspacing="0" width="100%%"
         style="background:#0a0a0a;padding:48px 24px">
    <tr><td align="center">
      <table role="presentation" cellpadding="0" cellspacing="0" width="100%%"
             style="max-width:480px">

        <!-- Logo -->
        <tr><td style="padding-bottom:32px">
          <span style="font-family:'Courier New',Courier,monospace;font-size:17px;
                        font-weight:700;letter-spacing:-0.01em;color:#F0EDE9">LLM</span><span
                style="font-family:system-ui,-apple-system,sans-serif;font-size:17px;
                       font-weight:600;color:#13A092">Rates</span>
        </td></tr>

        <!-- Heading -->
        <tr><td style="padding-bottom:12px">
          <p style="margin:0;font-size:22px;font-weight:700;letter-spacing:-0.02em;color:#F0EDE9">
            Verify your email
          </p>
        </td></tr>

        <!-- Body -->
        <tr><td style="padding-bottom:28px">
          <p style="margin:0;font-size:15px;line-height:1.65;color:#9a9690">
            Click the button below to confirm your address and get your free API key.
            The link expires shortly and can only be used once.
          </p>
        </td></tr>

        <!-- CTA -->
        <tr><td style="padding-bottom:32px">
          <a href="%s"
             style="display:inline-block;padding:12px 28px;background:#13A092;color:#F0EDE9;
                    text-decoration:none;font-size:15px;font-weight:600;letter-spacing:0.01em">
            Get my API key
          </a>
        </td></tr>

        <!-- Divider -->
        <tr><td style="border-top:1px solid #1e2020;padding-top:24px">
          <p style="margin:0;font-size:12px;line-height:1.6;color:#555">
            If you didn&rsquo;t request an API key, you can safely ignore this email.
          </p>
        </td></tr>

      </table>
    </td></tr>
  </table>
</body>
</html>`, magicLinkURL),
		Text: fmt.Sprintf(
			"LLMRates.live — verify your email\n\nOpen this link to get your free API key:\n%s\n\n"+
				"The link expires shortly and can only be used once.",
			magicLinkURL,
		),
	}
	_, err := m.client.Emails.SendWithContext(ctx, params)
	return err
}
