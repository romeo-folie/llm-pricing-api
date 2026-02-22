package billing

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"time"

	resend "github.com/resendlabs/resend-go"
)

//go:embed templates/*.html
var emailTemplates embed.FS

// EmailClient sends transactional emails via Resend.
type EmailClient struct {
	client    *resend.Client
	fromEmail string
	docsURL   string
	templates *template.Template
}

// NewEmailClient creates a new EmailClient using the provided Resend API key,
// sender address (e.g. "LLM Pricing <keys@llmrates.com>"), and docs URL linked
// in key delivery emails.
func NewEmailClient(apiKey, fromEmail, docsURL string) (*EmailClient, error) {
	return newEmailClient(http.DefaultClient, apiKey, fromEmail, docsURL)
}

// newEmailClient is the internal constructor that accepts a custom http.Client
// for test injection via resend.NewCustomClient.
func newEmailClient(httpClient *http.Client, apiKey, fromEmail, docsURL string) (*EmailClient, error) {
	tmpl, err := template.ParseFS(emailTemplates, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("billing: parse email templates: %w", err)
	}

	return &EmailClient{
		client:    resend.NewCustomClient(httpClient, apiKey),
		fromEmail: fromEmail,
		docsURL:   docsURL,
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
//
// ctx is accepted for interface consistency; the underlying Resend SDK (v1.7.0)
// does not propagate context into its HTTP calls.
func (c *EmailClient) SendKeyDelivery(_ context.Context, email, tier, keyValue string) error {
	data := keyDeliveryData{
		Tier:     tier,
		KeyValue: keyValue,
		DocsURL:  c.docsURL,
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
//
// ctx is accepted for interface consistency; the underlying Resend SDK (v1.7.0)
// does not propagate context into its HTTP calls.
func (c *EmailClient) SendPlanChange(_ context.Context, email, oldTier, newTier string, renewsAt time.Time) error {
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
//
// ctx is accepted for interface consistency; the underlying Resend SDK (v1.7.0)
// does not propagate context into its HTTP calls.
func (c *EmailClient) SendCancellation(_ context.Context, email string, renewsAt time.Time) error {
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
