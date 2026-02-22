package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient creates a LemonSqueezyClient with the HTTP client transport
// configured to redirect all requests to the provided test server.
// It works by setting a custom http.Client with a transport that rewrites
// the host to point at the test server, keeping the path intact.
func newTestClient(serverURL, apiKey, storeID string) *LemonSqueezyClient {
	transport := &testTransport{serverURL: serverURL}
	return &LemonSqueezyClient{
		apiKey:  apiKey,
		storeID: storeID,
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
	}
}

// testTransport redirects all HTTP requests to the provided serverURL,
// preserving the original path.
type testTransport struct {
	serverURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Build a new URL that points to the test server but keeps the original path.
	newReq := req.Clone(req.Context())
	newReq.URL.Scheme = "http"
	newReq.URL.Host = req.Host

	// Parse the test server URL to get host.
	parsed, err := http.NewRequest(http.MethodGet, t.serverURL, nil)
	if err != nil {
		return nil, err
	}
	newReq.URL.Host = parsed.URL.Host
	newReq.URL.Scheme = parsed.URL.Scheme
	return http.DefaultTransport.RoundTrip(newReq)
}

func TestGetSubscription_HappyPath(t *testing.T) {
	renewsAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	endsAt := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	expectedID := "sub_123"
	expectedEmail := "user@example.com"
	expectedStatus := "active"
	expectedVariantID := int64(456)
	expectedPortal := "https://app.lemonsqueezy.com/portal/abc"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{
			"data": map[string]any{
				"id": expectedID,
				"attributes": map[string]any{
					"status":     expectedStatus,
					"user_email": expectedEmail,
					"variant_id": expectedVariantID,
					"renews_at":  renewsAt.Format(time.RFC3339),
					"ends_at":    endsAt.Format(time.RFC3339),
					"urls": map[string]any{
						"customer_portal": expectedPortal,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL, "test-key", "store-1")
	sub, err := client.GetSubscription(context.Background(), expectedID)
	if err != nil {
		t.Fatalf("GetSubscription returned unexpected error: %v", err)
	}
	if sub.ID != expectedID {
		t.Errorf("ID: got %q, want %q", sub.ID, expectedID)
	}
	if sub.CustomerEmail != expectedEmail {
		t.Errorf("CustomerEmail: got %q, want %q", sub.CustomerEmail, expectedEmail)
	}
	if sub.Status != expectedStatus {
		t.Errorf("Status: got %q, want %q", sub.Status, expectedStatus)
	}
	if sub.VariantID != "456" {
		t.Errorf("VariantID: got %q, want %q", sub.VariantID, "456")
	}
	if sub.CustomerPortalURL != expectedPortal {
		t.Errorf("CustomerPortalURL: got %q, want %q", sub.CustomerPortalURL, expectedPortal)
	}
	if sub.RenewsAt == nil {
		t.Error("RenewsAt: got nil, want non-nil")
	} else if !sub.RenewsAt.UTC().Equal(renewsAt) {
		t.Errorf("RenewsAt: got %v, want %v", sub.RenewsAt, renewsAt)
	}
	if sub.EndsAt == nil {
		t.Error("EndsAt: got nil, want non-nil")
	} else if !sub.EndsAt.UTC().Equal(endsAt) {
		t.Errorf("EndsAt: got %v, want %v", sub.EndsAt, endsAt)
	}
}

func TestGetSubscription_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"detail":"Unauthenticated."}]}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL, "bad-key", "store-1")
	_, err := client.GetSubscription(context.Background(), "sub_999")
	if err == nil {
		t.Fatal("GetSubscription: expected error for non-OK status, got nil")
	}
}

func TestGetCustomerPortalURL_HappyPath(t *testing.T) {
	expectedURL := "https://app.lemonsqueezy.com/portal/session/xyz"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{
			"data": map[string]any{
				"attributes": map[string]any{
					"url": expectedURL,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL, "test-key", "store-1")
	url, err := client.GetCustomerPortalURL(context.Background(), "sub_123")
	if err != nil {
		t.Fatalf("GetCustomerPortalURL returned unexpected error: %v", err)
	}
	if url != expectedURL {
		t.Errorf("URL: got %q, want %q", url, expectedURL)
	}
}

func TestGetCustomerPortalURL_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"detail":"Forbidden"}]}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL, "bad-key", "store-1")
	_, err := client.GetCustomerPortalURL(context.Background(), "sub_123")
	if err == nil {
		t.Fatal("GetCustomerPortalURL: expected error for non-OK status, got nil")
	}
}
