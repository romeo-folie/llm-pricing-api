package handlers_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
	"llm-pricing-api/internal/billing"
)

// --- mock LemonSqueezyStore ---

type mockLSStore struct {
	tryInsertEventFunc   func(ctx context.Context, eventID, eventType string) (bool, error)
	createSubFunc        func(ctx context.Context, lsSubID, email, tier, unkeyKeyID string) error
	getSubFunc           func(ctx context.Context, lsSubID string) (handlers.BillingSubscription, error)
	updateTierFunc       func(ctx context.Context, lsSubID, tier string) error
	updateKeyFunc        func(ctx context.Context, lsSubID, unkeyKeyID string) error
	cancelSubFunc        func(ctx context.Context, lsSubID, revokeJobID string) error
	expireSubFunc        func(ctx context.Context, lsSubID string) error
	resumeSubFunc        func(ctx context.Context, lsSubID string) error
}

func (m *mockLSStore) TryInsertWebhookEvent(ctx context.Context, eventID, eventType string) (bool, error) {
	if m.tryInsertEventFunc != nil {
		return m.tryInsertEventFunc(ctx, eventID, eventType)
	}
	return true, nil // default: newly inserted
}

func (m *mockLSStore) CreateSubscription(ctx context.Context, lsSubID, email, tier, unkeyKeyID string) error {
	if m.createSubFunc != nil {
		return m.createSubFunc(ctx, lsSubID, email, tier, unkeyKeyID)
	}
	return nil
}

func (m *mockLSStore) GetSubscription(ctx context.Context, lsSubID string) (handlers.BillingSubscription, error) {
	if m.getSubFunc != nil {
		return m.getSubFunc(ctx, lsSubID)
	}
	return handlers.BillingSubscription{UnkeyKeyID: "uk_test", Email: "test@example.com", Tier: "developer"}, nil
}

func (m *mockLSStore) UpdateSubscriptionTier(ctx context.Context, lsSubID, tier string) error {
	if m.updateTierFunc != nil {
		return m.updateTierFunc(ctx, lsSubID, tier)
	}
	return nil
}

func (m *mockLSStore) UpdateSubscriptionKey(ctx context.Context, lsSubID, unkeyKeyID string) error {
	if m.updateKeyFunc != nil {
		return m.updateKeyFunc(ctx, lsSubID, unkeyKeyID)
	}
	return nil
}

func (m *mockLSStore) CancelSubscription(ctx context.Context, lsSubID, revokeJobID string) error {
	if m.cancelSubFunc != nil {
		return m.cancelSubFunc(ctx, lsSubID, revokeJobID)
	}
	return nil
}

func (m *mockLSStore) ExpireSubscription(ctx context.Context, lsSubID string) error {
	if m.expireSubFunc != nil {
		return m.expireSubFunc(ctx, lsSubID)
	}
	return nil
}

func (m *mockLSStore) ResumeSubscription(ctx context.Context, lsSubID string) error {
	if m.resumeSubFunc != nil {
		return m.resumeSubFunc(ctx, lsSubID)
	}
	return nil
}

// --- mock KeyManager ---

// mockKeyManager uses atomic booleans for the "called" flags to avoid data races
// when the handler dispatches in a background goroutine and the test polls from
// the main goroutine.
type mockKeyManager struct {
	createKeyFunc    func(ctx context.Context, email, tier string) (string, string, error)
	updateTierFunc   func(ctx context.Context, unkeyKeyID, tier string) error
	revokeKeyFunc    func(ctx context.Context, unkeyKeyID string) error
	createKeyCalled  atomic.Bool
	updateTierCalled atomic.Bool
	revokeKeyCalled  atomic.Bool
}

func (m *mockKeyManager) CreateKey(ctx context.Context, email, tier string) (string, string, error) {
	m.createKeyCalled.Store(true)
	if m.createKeyFunc != nil {
		return m.createKeyFunc(ctx, email, tier)
	}
	return "uk_test_id", "uk_test_key", nil
}

func (m *mockKeyManager) UpdateKeyTier(ctx context.Context, unkeyKeyID, tier string) error {
	m.updateTierCalled.Store(true)
	if m.updateTierFunc != nil {
		return m.updateTierFunc(ctx, unkeyKeyID, tier)
	}
	return nil
}

func (m *mockKeyManager) RevokeKey(ctx context.Context, unkeyKeyID string) error {
	m.revokeKeyCalled.Store(true)
	if m.revokeKeyFunc != nil {
		return m.revokeKeyFunc(ctx, unkeyKeyID)
	}
	return nil
}

// --- mock Emailer ---

// mockEmailer uses atomic booleans for race-safe polling in dispatch tests.
type mockEmailer struct {
	sendDeliveryFunc     func(ctx context.Context, email, tier, keyValue string) error
	sendPlanChangeFunc   func(ctx context.Context, email, oldTier, newTier string, renewsAt time.Time) error
	sendCancellationFunc func(ctx context.Context, email string, renewsAt time.Time) error
	deliveryCalled       atomic.Bool
	planChangeCalled     atomic.Bool
}

func (m *mockEmailer) SendKeyDelivery(ctx context.Context, email, tier, keyValue string) error {
	m.deliveryCalled.Store(true)
	if m.sendDeliveryFunc != nil {
		return m.sendDeliveryFunc(ctx, email, tier, keyValue)
	}
	return nil
}

func (m *mockEmailer) SendPlanChange(ctx context.Context, email, oldTier, newTier string, renewsAt time.Time) error {
	m.planChangeCalled.Store(true)
	if m.sendPlanChangeFunc != nil {
		return m.sendPlanChangeFunc(ctx, email, oldTier, newTier, renewsAt)
	}
	return nil
}

func (m *mockEmailer) SendCancellation(ctx context.Context, email string, renewsAt time.Time) error {
	if m.sendCancellationFunc != nil {
		return m.sendCancellationFunc(ctx, email, renewsAt)
	}
	return nil
}

// --- helpers ---

const (
	testSigningSecret = "test-secret"
	testVariantDev    = "11111"
	testVariantPro    = "22222"
)

// signBody computes the HMAC-SHA256 signature for a request body.
func signBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// newLSApp creates a minimal Fiber app wired with the LemonSqueezy webhook route.
func newLSApp(store handlers.LemonSqueezyStore, keys billing.KeyManager, emailer billing.Emailer,
	enqueue func(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error),
	deleteTask func(queue, taskID string) error,
) *fiber.App {
	app := fiber.New()

	h := handlers.NewLemonSqueezyHandler(handlers.LemonSqueezyHandlerConfig{
		Store:         store,
		Keys:          keys,
		Email:         emailer,
		EnqueueTask:   enqueue,
		DeleteTask:    deleteTask,
		SigningSecret: testSigningSecret,
		VariantDev:    testVariantDev,
		VariantPro:    testVariantPro,
	})
	app.Post("/webhooks/lemon-squeezy", h.Handle)

	return app
}

// lsRequest fires a signed POST to /webhooks/lemon-squeezy and returns status+body.
func lsRequest(app *fiber.App, body []byte, sig string) (int, []byte) {
	req := httptest.NewRequest("POST", "/webhooks/lemon-squeezy", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sig != "" {
		req.Header.Set("X-Signature", sig)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		panic("app.Test: " + err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// buildPayload constructs a minimal LS webhook JSON payload.
func buildPayload(eventName, webhookID, subID, email string, variantID int) []byte {
	p := map[string]any{
		"meta": map[string]any{
			"event_name": eventName,
			"webhook_id": webhookID,
		},
		"data": map[string]any{
			"id": subID,
			"attributes": map[string]any{
				"user_email": email,
				"variant_id": variantID,
				"status":     "active",
				"renews_at":  time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
				"ends_at":    nil,
			},
		},
	}
	b, _ := json.Marshal(p)
	return b
}

// waitDispatch waits up to 100ms for the background goroutine to execute the
// given check function. Returns true when check() returns true.
func waitDispatch(check func() bool) bool {
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// --- tests ---

func TestLemonSqueezy_ValidPayload_ValidSig_Returns200(t *testing.T) {
	store := &mockLSStore{}
	keys := &mockKeyManager{}
	emailer := &mockEmailer{}

	app := newLSApp(store, keys, emailer, nil, nil)

	body := buildPayload("subscription_created", "wh_001", "sub_1", "user@example.com", 11111)
	sig := signBody(body, testSigningSecret)

	status, _ := lsRequest(app, body, sig)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
}

func TestLemonSqueezy_ValidPayload_BadSig_Returns403(t *testing.T) {
	store := &mockLSStore{}
	keys := &mockKeyManager{}
	emailer := &mockEmailer{}

	app := newLSApp(store, keys, emailer, nil, nil)

	body := buildPayload("subscription_created", "wh_002", "sub_2", "user@example.com", 11111)
	status, _ := lsRequest(app, body, "deadbeef")

	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403 for bad signature, got %d", status)
	}
}

func TestLemonSqueezy_MissingSignature_Returns403(t *testing.T) {
	store := &mockLSStore{}
	app := newLSApp(store, &mockKeyManager{}, &mockEmailer{}, nil, nil)

	body := buildPayload("subscription_created", "wh_003", "sub_3", "user@example.com", 11111)
	status, _ := lsRequest(app, body, "")

	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403 for missing signature, got %d", status)
	}
}

func TestLemonSqueezy_DuplicateEventID_Returns200NoOp(t *testing.T) {
	store := &mockLSStore{
		tryInsertEventFunc: func(_ context.Context, _, _ string) (bool, error) {
			return false, nil // duplicate: ON CONFLICT DO NOTHING → 0 rows affected
		},
	}
	keys := &mockKeyManager{}
	app := newLSApp(store, keys, &mockEmailer{}, nil, nil)

	body := buildPayload("subscription_created", "wh_004", "sub_4", "user@example.com", 11111)
	sig := signBody(body, testSigningSecret)
	status, _ := lsRequest(app, body, sig)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for duplicate event, got %d", status)
	}
	// Give the goroutine time to potentially fire (it should not).
	time.Sleep(50 * time.Millisecond)
	if keys.createKeyCalled.Load() {
		t.Error("expected CreateKey not to be called for duplicate events")
	}
}

func TestLemonSqueezy_SubscriptionCreated_CallsCreateKeyAndEmail(t *testing.T) {
	store := &mockLSStore{}
	keys := &mockKeyManager{}
	emailer := &mockEmailer{}

	app := newLSApp(store, keys, emailer, nil, nil)

	body := buildPayload("subscription_created", "wh_005", "sub_5", "user@example.com", 11111)
	sig := signBody(body, testSigningSecret)
	status, _ := lsRequest(app, body, sig)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if !waitDispatch(func() bool { return keys.createKeyCalled.Load() && emailer.deliveryCalled.Load() }) {
		t.Errorf("expected CreateKey=%v and SendKeyDelivery=%v to be called",
			keys.createKeyCalled.Load(), emailer.deliveryCalled.Load())
	}
}

func TestLemonSqueezy_SubscriptionUpdated_CallsUpdateKeyTierAndEmail(t *testing.T) {
	store := &mockLSStore{}
	keys := &mockKeyManager{}
	emailer := &mockEmailer{}

	app := newLSApp(store, keys, emailer, nil, nil)

	body := buildPayload("subscription_updated", "wh_006", "sub_6", "user@example.com", 22222)
	sig := signBody(body, testSigningSecret)
	status, _ := lsRequest(app, body, sig)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if !waitDispatch(func() bool { return keys.updateTierCalled.Load() && emailer.planChangeCalled.Load() }) {
		t.Errorf("expected UpdateKeyTier=%v and SendPlanChange=%v to be called",
			keys.updateTierCalled.Load(), emailer.planChangeCalled.Load())
	}
}

func TestLemonSqueezy_SubscriptionCancelled_EnqueuesRevokeJob(t *testing.T) {
	store := &mockLSStore{
		getSubFunc: func(_ context.Context, _ string) (handlers.BillingSubscription, error) {
			return handlers.BillingSubscription{UnkeyKeyID: "uk_test", Email: "user@example.com", Tier: "pro"}, nil
		},
	}
	keys := &mockKeyManager{}
	var enqueueCalled atomic.Bool

	enqueue := func(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
		enqueueCalled.Store(true)
		if task.Type() != handlers.TypeBillingRevokeKey {
			return nil, fmt.Errorf("unexpected task type: %s", task.Type())
		}
		return &asynq.TaskInfo{ID: "task_123"}, nil
	}

	app := newLSApp(store, keys, &mockEmailer{}, enqueue, nil)

	body := buildPayload("subscription_cancelled", "wh_007", "sub_7", "user@example.com", 22222)
	sig := signBody(body, testSigningSecret)
	status, _ := lsRequest(app, body, sig)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if !waitDispatch(func() bool { return enqueueCalled.Load() }) {
		t.Error("expected enqueue to be called for subscription_cancelled")
	}
}

func TestLemonSqueezy_SubscriptionExpired_CallsRevokeKey(t *testing.T) {
	store := &mockLSStore{}
	keys := &mockKeyManager{}

	app := newLSApp(store, keys, &mockEmailer{}, nil, nil)

	body := buildPayload("subscription_expired", "wh_008", "sub_8", "user@example.com", 22222)
	sig := signBody(body, testSigningSecret)
	status, _ := lsRequest(app, body, sig)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if !waitDispatch(func() bool { return keys.revokeKeyCalled.Load() }) {
		t.Error("expected RevokeKey to be called for subscription_expired")
	}
}

func TestLemonSqueezy_SubscriptionResumed_CancelsRevokeJob(t *testing.T) {
	revokeJobID := "task_456"
	store := &mockLSStore{
		getSubFunc: func(_ context.Context, _ string) (handlers.BillingSubscription, error) {
			return handlers.BillingSubscription{
				UnkeyKeyID:  "uk_test",
				Email:       "user@example.com",
				Tier:        "pro",
				RevokeJobID: &revokeJobID,
			}, nil
		},
	}
	var deleteTaskCalled atomic.Bool
	deleteTask := func(queue, taskID string) error {
		deleteTaskCalled.Store(true)
		if taskID != revokeJobID {
			return fmt.Errorf("unexpected taskID: %s", taskID)
		}
		return nil
	}

	app := newLSApp(store, &mockKeyManager{}, &mockEmailer{}, nil, deleteTask)

	body := buildPayload("subscription_resumed", "wh_009", "sub_9", "user@example.com", 22222)
	sig := signBody(body, testSigningSecret)
	status, _ := lsRequest(app, body, sig)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if !waitDispatch(func() bool { return deleteTaskCalled.Load() }) {
		t.Error("expected deleteTask to be called for subscription_resumed")
	}
}

func TestLemonSqueezy_UnknownEventType_Returns200(t *testing.T) {
	store := &mockLSStore{}
	app := newLSApp(store, &mockKeyManager{}, &mockEmailer{}, nil, nil)

	body := buildPayload("subscription_paused", "wh_010", "sub_10", "user@example.com", 22222)
	sig := signBody(body, testSigningSecret)
	status, _ := lsRequest(app, body, sig)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for unknown event type, got %d", status)
	}
}

func TestLemonSqueezy_SubscriptionResumed_AlreadyExpired_RecreatesKey(t *testing.T) {
	// When subscription_resumed arrives after the revoke-key task already ran
	// (status == 'expired'), a new Unkey key must be created and delivered.
	store := &mockLSStore{
		getSubFunc: func(_ context.Context, _ string) (handlers.BillingSubscription, error) {
			return handlers.BillingSubscription{
				UnkeyKeyID: "uk_revoked",
				Email:      "user@example.com",
				Tier:       "pro",
				Status:     "expired", // revoke task already ran
			}, nil
		},
	}
	keys := &mockKeyManager{}
	emailer := &mockEmailer{}

	app := newLSApp(store, keys, emailer, nil, nil)

	body := buildPayload("subscription_resumed", "wh_013", "sub_13", "user@example.com", 22222)
	sig := signBody(body, testSigningSecret)
	status, _ := lsRequest(app, body, sig)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if !waitDispatch(func() bool { return keys.createKeyCalled.Load() && emailer.deliveryCalled.Load() }) {
		t.Errorf("expected CreateKey=%v and SendKeyDelivery=%v after reactivation of expired subscription",
			keys.createKeyCalled.Load(), emailer.deliveryCalled.Load())
	}
}

func TestLemonSqueezy_SubscriptionUpdated_SameTier_NoEmailOrKeyUpdate(t *testing.T) {
	// subscription_updated with the same variant as the stored tier must not send
	// a plan-change email or call UpdateKeyTier.
	store := &mockLSStore{
		getSubFunc: func(_ context.Context, _ string) (handlers.BillingSubscription, error) {
			// Return the same tier as the incoming variant (developer / 11111).
			return handlers.BillingSubscription{UnkeyKeyID: "uk_test", Email: "user@example.com", Tier: "developer"}, nil
		},
	}
	keys := &mockKeyManager{}
	emailer := &mockEmailer{}

	app := newLSApp(store, keys, emailer, nil, nil)

	// Variant 11111 → "developer", same as stored tier.
	body := buildPayload("subscription_updated", "wh_011", "sub_11", "user@example.com", 11111)
	sig := signBody(body, testSigningSecret)
	status, _ := lsRequest(app, body, sig)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	// Give the goroutine 100ms to complete and then verify nothing was called.
	time.Sleep(100 * time.Millisecond)
	if keys.updateTierCalled.Load() {
		t.Error("expected UpdateKeyTier NOT to be called when tier is unchanged")
	}
	if emailer.planChangeCalled.Load() {
		t.Error("expected SendPlanChange NOT to be called when tier is unchanged")
	}
}

func TestLemonSqueezy_SubscriptionCreated_StoreFailure_RevokesOrphanedKey(t *testing.T) {
	// When CreateSubscription fails after CreateKey succeeds, the handler must
	// attempt to revoke the orphaned Unkey key.
	var revokeKeyCalled atomic.Bool
	store := &mockLSStore{
		createSubFunc: func(_ context.Context, _, _, _, _ string) error {
			return fmt.Errorf("db error")
		},
	}
	keys := &mockKeyManager{
		revokeKeyFunc: func(_ context.Context, _ string) error {
			revokeKeyCalled.Store(true)
			return nil
		},
	}
	emailer := &mockEmailer{}

	app := newLSApp(store, keys, emailer, nil, nil)

	body := buildPayload("subscription_created", "wh_012", "sub_12", "user@example.com", 11111)
	sig := signBody(body, testSigningSecret)
	status, _ := lsRequest(app, body, sig)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200 (handler always returns 200), got %d", status)
	}

	if !waitDispatch(func() bool { return revokeKeyCalled.Load() }) {
		t.Error("expected RevokeKey to be called to compensate for orphaned key after store failure")
	}
}

// ---------------------------------------------------------------------------
// FreeSignupHandler tests
// ---------------------------------------------------------------------------

// mockFreeKeys implements billing.KeyManager for FreeSignupHandler tests.
type mockFreeKeys struct {
	createKeyFunc func(ctx context.Context, email, tier string) (string, string, error)
}

func (m *mockFreeKeys) CreateKey(ctx context.Context, email, tier string) (string, string, error) {
	if m.createKeyFunc != nil {
		return m.createKeyFunc(ctx, email, tier)
	}
	return "key-id", "uk_free_test", nil
}

func (m *mockFreeKeys) UpdateKeyTier(ctx context.Context, id, tier string) error { return nil }
func (m *mockFreeKeys) RevokeKey(ctx context.Context, id string) error            { return nil }

// mockFreeEmail implements billing.Emailer for FreeSignupHandler tests.
type mockFreeEmail struct {
	sendKeyFunc func(ctx context.Context, email, tier, keyValue string) error
}

func (m *mockFreeEmail) SendKeyDelivery(ctx context.Context, email, tier, keyValue string) error {
	if m.sendKeyFunc != nil {
		return m.sendKeyFunc(ctx, email, tier, keyValue)
	}
	return nil
}

func (m *mockFreeEmail) SendPlanChange(ctx context.Context, email, oldTier, newTier string, renewsAt time.Time) error {
	return nil
}

func (m *mockFreeEmail) SendCancellation(ctx context.Context, email string, renewsAt time.Time) error {
	return nil
}

// newFreeSignupApp builds a minimal Fiber app wired with FreeSignupHandler.
// It uses the api.ErrorHandler so RFC 7807 problem responses are serialised correctly.
func newFreeSignupApp(t *testing.T, keys billing.KeyManager, emailer billing.Emailer, rdbAddr string) *fiber.App {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: rdbAddr})
	t.Cleanup(func() { _ = rdb.Close() })

	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	h := handlers.NewFreeSignupHandler(keys, emailer, rdb, zerolog.Nop())
	app.Post("/v1/signup/free", h.Handle)
	return app
}

// freeSignupPost fires a POST to /v1/signup/free and returns status + body bytes.
func freeSignupPost(app *fiber.App, body string) (int, []byte) {
	req := httptest.NewRequest("POST", "/v1/signup/free", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		panic("app.Test: " + err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// freeSignupPostHeaders fires a POST and returns status, body, and the response headers.
func freeSignupPostHeaders(app *fiber.App, body string) (int, []byte, map[string]string) {
	req := httptest.NewRequest("POST", "/v1/signup/free", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		panic("app.Test: " + err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	headers := make(map[string]string)
	for k, vs := range resp.Header {
		if len(vs) > 0 {
			headers[k] = vs[0]
		}
	}
	return resp.StatusCode, b, headers
}

func TestFreeSignupHandler(t *testing.T) {
	t.Run("valid email returns 200 with message and no key in body", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("miniredis.Run: %v", err)
		}
		t.Cleanup(mr.Close)

		var capturedKeyValue string
		keys := &mockFreeKeys{
			createKeyFunc: func(_ context.Context, email, tier string) (string, string, error) {
				capturedKeyValue = "uk_test_abc"
				return "key-id-123", "uk_test_abc", nil
			},
		}
		emailer := &mockFreeEmail{}

		app := newFreeSignupApp(t, keys, emailer, mr.Addr())
		status, body := freeSignupPost(app, `{"email":"user@example.com"}`)

		if status != 200 {
			t.Fatalf("expected 200, got %d; body: %s", status, body)
		}

		var resp map[string]any
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("response is not valid JSON: %v; body: %s", err, body)
		}
		if _, ok := resp["message"]; !ok {
			t.Errorf("expected 'message' key in response body, got: %s", body)
		}
		if strings.Contains(string(body), capturedKeyValue) {
			t.Errorf("response body must not contain the key value %q; body: %s", capturedKeyValue, body)
		}
	})

	t.Run("invalid email returns 400 with problem+json content type", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("miniredis.Run: %v", err)
		}
		t.Cleanup(mr.Close)

		app := newFreeSignupApp(t, &mockFreeKeys{}, &mockFreeEmail{}, mr.Addr())
		req := httptest.NewRequest("POST", "/v1/signup/free", strings.NewReader(`{"email":"notanemail"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != 400 {
			t.Fatalf("expected 400, got %d; body: %s", resp.StatusCode, body)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/problem+json") {
			t.Errorf("expected Content-Type application/problem+json, got %q", ct)
		}
	})

	t.Run("empty email returns 400", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("miniredis.Run: %v", err)
		}
		t.Cleanup(mr.Close)

		app := newFreeSignupApp(t, &mockFreeKeys{}, &mockFreeEmail{}, mr.Addr())
		status, body := freeSignupPost(app, `{"email":""}`)

		if status != 400 {
			t.Fatalf("expected 400 for empty email, got %d; body: %s", status, body)
		}
	})

	t.Run("4th request from same IP returns 429 with Retry-After header and problem+json", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("miniredis.Run: %v", err)
		}
		t.Cleanup(mr.Close)

		keys := &mockFreeKeys{
			createKeyFunc: func(_ context.Context, _, _ string) (string, string, error) {
				return "key-id", "uk_test_key", nil
			},
		}
		app := newFreeSignupApp(t, keys, &mockFreeEmail{}, mr.Addr())

		// First 3 requests should succeed.
		for i := 1; i <= 3; i++ {
			status, body := freeSignupPost(app, `{"email":"user@example.com"}`)
			if status != 200 {
				t.Fatalf("request %d: expected 200, got %d; body: %s", i, status, body)
			}
		}

		// 4th request must be rejected.
		status, body, headers := freeSignupPostHeaders(app, `{"email":"user@example.com"}`)

		if status != 429 {
			t.Fatalf("4th request: expected 429, got %d; body: %s", status, body)
		}
		retryAfter := headers["Retry-After"]
		if retryAfter != "86400" {
			t.Errorf("expected Retry-After: 86400, got %q", retryAfter)
		}
		ct := headers["Content-Type"]
		if !strings.Contains(ct, "application/problem+json") {
			t.Errorf("expected Content-Type application/problem+json on 429, got %q", ct)
		}
	})

	t.Run("email send failure still returns 200 with message and no key value", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("miniredis.Run: %v", err)
		}
		t.Cleanup(mr.Close)

		const issuedKey = "uk_test_should_not_appear"
		keys := &mockFreeKeys{
			createKeyFunc: func(_ context.Context, _, _ string) (string, string, error) {
				return "key-id-456", issuedKey, nil
			},
		}
		emailer := &mockFreeEmail{
			sendKeyFunc: func(_ context.Context, _, _, _ string) error {
				return fmt.Errorf("SMTP timeout")
			},
		}

		app := newFreeSignupApp(t, keys, emailer, mr.Addr())
		status, body := freeSignupPost(app, `{"email":"user@example.com"}`)

		if status != 200 {
			t.Fatalf("expected 200 even when email fails, got %d; body: %s", status, body)
		}
		var resp map[string]any
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("response is not valid JSON: %v; body: %s", err, body)
		}
		if _, ok := resp["message"]; !ok {
			t.Errorf("expected 'message' key in response body, got: %s", body)
		}
		if strings.Contains(string(body), issuedKey) {
			t.Errorf("response body must not contain the key value %q; body: %s", issuedKey, body)
		}
	})
}
