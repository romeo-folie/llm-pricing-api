// Package billing provides clients for external billing and API-key management services.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	lsBaseURL = "https://api.lemonsqueezy.com/v1"
	lsTimeout = 10 * time.Second
)

// LemonSqueezyClient wraps the Lemon Squeezy REST API.
// It must be constructed via NewLemonSqueezyClient; the zero value is not usable.
type LemonSqueezyClient struct {
	apiKey     string
	httpClient *http.Client
}

// Subscription is the normalised representation of a Lemon Squeezy subscription.
type Subscription struct {
	// ID is the Lemon Squeezy subscription identifier.
	ID string
	// CustomerEmail is the email address of the subscriber.
	CustomerEmail string
	// Status is the current subscription status (e.g. "active", "cancelled").
	Status string
	// VariantID is the Lemon Squeezy product variant identifier.
	VariantID string
	// EndsAt is the timestamp at which the subscription ends (nil if not set).
	EndsAt *time.Time
	// RenewsAt is the timestamp of the next renewal (nil if not set).
	RenewsAt *time.Time
	// CustomerPortalURL is the self-serve management URL for the subscriber.
	CustomerPortalURL string
}

// NewLemonSqueezyClient constructs a LemonSqueezyClient with the supplied API key.
func NewLemonSqueezyClient(apiKey string) *LemonSqueezyClient {
	return &LemonSqueezyClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: lsTimeout,
		},
	}
}

// GetSubscription fetches a subscription by its Lemon Squeezy subscription ID.
// It returns the normalised Subscription or an error with the HTTP status code when
// the upstream call fails.
func (c *LemonSqueezyClient) GetSubscription(ctx context.Context, lsSubscriptionID string) (*Subscription, error) {
	if err := validateSubscriptionID(lsSubscriptionID); err != nil {
		return nil, fmt.Errorf("billing: get subscription: %w", err)
	}

	url := fmt.Sprintf("%s/subscriptions/%s", lsBaseURL, lsSubscriptionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("billing: get subscription: build request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("billing: get subscription: http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("billing: get subscription: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("billing: get subscription: upstream returned status %d: %s", resp.StatusCode, truncate(body, 200))
	}

	var envelope lsSubscriptionEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("billing: get subscription: decode response: %w", err)
	}

	return envelope.toSubscription(), nil
}

// GetCustomerPortalURL returns the 24-hour Lemon Squeezy self-serve portal URL
// for the given subscription. This URL allows the customer to manage billing
// details, upgrade, downgrade, or cancel their subscription.
func (c *LemonSqueezyClient) GetCustomerPortalURL(ctx context.Context, lsSubscriptionID string) (string, error) {
	if err := validateSubscriptionID(lsSubscriptionID); err != nil {
		return "", fmt.Errorf("billing: get customer portal URL: %w", err)
	}

	url := fmt.Sprintf("%s/subscriptions/%s/customer-portal-session", lsBaseURL, lsSubscriptionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return "", fmt.Errorf("billing: get customer portal URL: build request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/vnd.api+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("billing: get customer portal URL: http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("billing: get customer portal URL: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("billing: get customer portal URL: upstream returned status %d: %s", resp.StatusCode, truncate(body, 200))
	}

	var portalResp lsPortalSessionResponse
	if err := json.Unmarshal(body, &portalResp); err != nil {
		return "", fmt.Errorf("billing: get customer portal URL: decode response: %w", err)
	}

	if portalResp.Data.Attributes.URL == "" {
		return "", fmt.Errorf("billing: get customer portal URL: empty URL in response")
	}

	return portalResp.Data.Attributes.URL, nil
}

// validateSubscriptionID rejects values that could inject unexpected path segments
// or query parameters into the Lemon Squeezy API URL.
func validateSubscriptionID(id string) error {
	if id == "" || strings.ContainsAny(id, "/?#&%") {
		return fmt.Errorf("invalid subscription ID %q", id)
	}
	return nil
}

// setHeaders attaches the required Lemon Squeezy authentication and accept headers to r.
func (c *LemonSqueezyClient) setHeaders(r *http.Request) {
	r.Header.Set("Authorization", "Bearer "+c.apiKey)
	r.Header.Set("Accept", "application/vnd.api+json")
}

// truncate returns at most n runes of b as a string, for safe use in error messages.
// It slices on rune boundaries to avoid producing invalid UTF-8.
func truncate(b []byte, n int) string {
	runes := []rune(string(b))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n]) + "..."
}

// ---------------------------------------------------------------------------
// Internal JSON shapes for Lemon Squeezy API responses.
// These are unexported; callers receive the normalised Subscription type.
// ---------------------------------------------------------------------------

type lsSubscriptionEnvelope struct {
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			Status    string     `json:"status"`
			UserEmail string     `json:"user_email"`
			VariantID int64      `json:"variant_id"`
			EndsAt    *time.Time `json:"ends_at"`
			RenewsAt  *time.Time `json:"renews_at"`
			Urls      struct {
				CustomerPortal string `json:"customer_portal"`
			} `json:"urls"`
		} `json:"attributes"`
	} `json:"data"`
}

func (e *lsSubscriptionEnvelope) toSubscription() *Subscription {
	attr := e.Data.Attributes
	return &Subscription{
		ID:                e.Data.ID,
		CustomerEmail:     attr.UserEmail,
		Status:            attr.Status,
		VariantID:         fmt.Sprintf("%d", attr.VariantID),
		EndsAt:            attr.EndsAt,
		RenewsAt:          attr.RenewsAt,
		CustomerPortalURL: attr.Urls.CustomerPortal,
	}
}

type lsPortalSessionResponse struct {
	Data struct {
		Attributes struct {
			URL string `json:"url"`
		} `json:"attributes"`
	} `json:"data"`
}
