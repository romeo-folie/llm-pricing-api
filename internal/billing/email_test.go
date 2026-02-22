package billing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newMockEmailClient creates an EmailClient whose HTTP calls are redirected to
// the provided test server, so no real network traffic is generated. The
// testTransport type is defined in client_test.go (same package).
func newMockEmailClient(t *testing.T, srv *httptest.Server) *EmailClient {
	t.Helper()
	transport := &testTransport{serverURL: srv.URL}
	client, err := newEmailClient(&http.Client{Transport: transport}, "dummy-key", "from@example.com", "https://example.com/docs")
	if err != nil {
		t.Fatalf("newEmailClient returned unexpected error: %v", err)
	}
	return client
}

func TestNewEmailClient_Success(t *testing.T) {
	// Template parsing happens at construction time; no network call is made.
	client, err := newEmailClient(http.DefaultClient, "dummy-key", "from@example.com", "https://example.com/docs")
	if err != nil {
		t.Fatalf("newEmailClient returned unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("newEmailClient returned nil client")
	}
}

func TestSendKeyDelivery_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"statusCode":401,"error":"Unauthorized","message":"API key is invalid"}`))
	}))
	defer srv.Close()

	client := newMockEmailClient(t, srv)
	err := client.SendKeyDelivery(context.Background(), "to@example.com", "Developer", "test_key_value")
	if err == nil {
		t.Fatal("SendKeyDelivery: expected error for 401 response, got nil")
	}
}

func TestSendPlanChange_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"statusCode":401,"error":"Unauthorized","message":"API key is invalid"}`))
	}))
	defer srv.Close()

	client := newMockEmailClient(t, srv)
	renewsAt := time.Now().Add(30 * 24 * time.Hour)
	err := client.SendPlanChange(context.Background(), "to@example.com", "Developer", "Pro", renewsAt)
	if err == nil {
		t.Fatal("SendPlanChange: expected error for 401 response, got nil")
	}
}

func TestSendCancellation_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"statusCode":401,"error":"Unauthorized","message":"API key is invalid"}`))
	}))
	defer srv.Close()

	client := newMockEmailClient(t, srv)
	accessUntil := time.Now().Add(30 * 24 * time.Hour)
	err := client.SendCancellation(context.Background(), "to@example.com", accessUntil)
	if err == nil {
		t.Fatal("SendCancellation: expected error for 401 response, got nil")
	}
}
