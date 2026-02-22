package billing

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"time"

	resend "github.com/resendlabs/resend-go"
)

//go:embed templates/*.html
var emailTemplates embed.FS

// EmailClient sends transactional emails via Resend.
type EmailClient struct {
	client    *resend.Client
	fromEmail string
	templates *template.Template
}

// NewEmailClient creates a new EmailClient using the provided Resend API key
// and sender address (e.g. "LLM Pricing <no-reply@llmpricing.dev>").
func NewEmailClient(apiKey, fromEmail string) (*EmailClient, error) {
	tmpl, err := template.ParseFS(emailTemplates, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("billing: parse email templates: %w", err)
	}

	return &EmailClient{
		client:    resend.NewClient(apiKey),
		fromEmail: fromEmail,
		templates: tmpl,
	}, nil
}

// keyDeliveryData holds the template variables for the API key delivery email.
type keyDeliveryData struct {
	Tier     string
	KeyValue string
	DocsURL  string
}

// planChangeData holds the template variables for the plan change notification email.
type planChangeData struct {
	OldTier  string
	NewTier  string
	RenewsAt time.Time
}

// cancellationData holds the template variables for the cancellation notification email.
type cancellationData struct {
	Email       string
	AccessUntil time.Time
}

// SendKeyDelivery sends a welcome email containing the new API key to the
// subscriber. The key value is shown once — the recipient must save it.
func (c *EmailClient) SendKeyDelivery(email, tier, keyValue string) error {
	data := keyDeliveryData{
		Tier:     tier,
		KeyValue: keyValue,
		DocsURL:  "https://llmpricing.dev/docs",
	}

	body, err := c.render("key_delivery.html", data)
	if err != nil {
		return fmt.Errorf("billing: send key_delivery email: %w", err)
	}

	subject := fmt.Sprintf("Your %s API Key", tier)
	return c.send(email, subject, body)
}

// SendPlanChange sends a notification when a subscription tier changes, listing
// the old and new tiers and the next renewal date.
func (c *EmailClient) SendPlanChange(email, oldTier, newTier string, renewsAt time.Time) error {
	data := planChangeData{
		OldTier:  oldTier,
		NewTier:  newTier,
		RenewsAt: renewsAt,
	}

	body, err := c.render("plan_change.html", data)
	if err != nil {
		return fmt.Errorf("billing: send plan_change email: %w", err)
	}

	subject := fmt.Sprintf("Your plan has been updated to %s", newTier)
	return c.send(email, subject, body)
}

// SendCancellation sends a notification when a subscription is cancelled.
// Access continues until the renewsAt date already paid for.
func (c *EmailClient) SendCancellation(email string, renewsAt time.Time) error {
	data := cancellationData{
		Email:       email,
		AccessUntil: renewsAt,
	}

	body, err := c.render("cancellation.html", data)
	if err != nil {
		return fmt.Errorf("billing: send cancellation email: %w", err)
	}

	return c.send(email, "Your subscription has been cancelled", body)
}

// render executes the named HTML template with the provided data and returns
// the rendered HTML string.
func (c *EmailClient) render(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := c.templates.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("render template %q: %w", name, err)
	}
	return buf.String(), nil
}

// send delivers a single HTML email via the Resend API.
func (c *EmailClient) send(to, subject, htmlBody string) error {
	params := &resend.SendEmailRequest{
		From:    c.fromEmail,
		To:      []string{to},
		Subject: subject,
		Html:    htmlBody,
	}

	_, err := c.client.Emails.Send(params)
	if err != nil {
		return fmt.Errorf("billing: resend send: %w", err)
	}
	return nil
}
