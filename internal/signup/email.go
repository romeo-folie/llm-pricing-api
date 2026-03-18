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
		Subject: "Your LLMRates API key",
		Html: fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="color-scheme" content="light">
</head>
<body style="margin:0;padding:0;background:#F2EDE8;color:#1C1917;
             font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',system-ui,sans-serif">
  <table role="presentation" cellpadding="0" cellspacing="0" width="100%%"
         style="background:#F2EDE8;padding:48px 24px">
    <tr><td align="center">
      <table role="presentation" cellpadding="0" cellspacing="0" width="100%%"
             style="max-width:480px">

        <!-- Logo -->
        <tr><td style="padding-bottom:40px">
          <span style="font-family:'Courier New',Courier,monospace;font-size:16px;
                       font-weight:700;letter-spacing:0.04em;color:#1C1917">LLM</span><span
                style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',system-ui,sans-serif;
                       font-size:16px;font-weight:600;color:#107E72">Rates</span>
        </td></tr>

        <!-- Divider -->
        <tr><td style="padding-bottom:32px;border-top:1px solid #DDD7D0"></td></tr>

        <!-- Heading -->
        <tr><td style="padding-bottom:16px">
          <p style="margin:0;font-family:'Courier New',Courier,monospace;
                    font-size:24px;font-weight:700;letter-spacing:-0.01em;
                    color:#1C1917;line-height:1.2">
            Your API key is ready.
          </p>
        </td></tr>

        <!-- Body -->
        <tr><td style="padding-bottom:32px">
          <p style="margin:0;font-size:15px;line-height:1.65;color:#78716C">
            Click the button below to verify your email and collect your free API key.
            This link expires shortly and can only be used once.
          </p>
        </td></tr>

        <!-- CTA -->
        <tr><td style="padding-bottom:40px">
          <a href="%s"
             style="display:inline-block;padding:13px 28px;
                    background:#107E72;color:#FDFAF7;
                    text-decoration:none;font-size:14px;font-weight:600;
                    letter-spacing:0.02em;font-family:'Courier New',Courier,monospace">
            Get my API key &rarr;
          </a>
        </td></tr>

        <!-- Divider -->
        <tr><td style="border-top:1px solid #DDD7D0;padding-top:24px">
          <p style="margin:0;font-size:12px;line-height:1.6;color:#A8A29E">
            If you didn&rsquo;t request this, you can safely ignore it.
            This link will expire automatically.
          </p>
        </td></tr>

      </table>
    </td></tr>
  </table>
</body>
</html>`, magicLinkURL),
		Text: fmt.Sprintf(
			"LLMRates — your API key is ready.\n\n"+
				"Click the link below to verify your email and collect your free API key.\n"+
				"This link expires shortly and can only be used once.\n\n%s\n\n"+
				"If you didn't request this, you can safely ignore it.",
			magicLinkURL,
		),
	}
	_, err := m.client.Emails.SendWithContext(ctx, params)
	return err
}
