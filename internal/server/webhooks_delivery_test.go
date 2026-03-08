package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
)

func TestWebhookDeliveryRetriesThenSucceeds(t *testing.T) {
	var (
		mu           sync.Mutex
		requestCount int
		lastHeaders  http.Header
	)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requestCount++
		lastHeaders = r.Header.Clone()
		if requestCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	manager := newWebhookManager(&Server{}, filepath.Join(t.TempDir(), "state.json"))
	webhook, _, err := manager.createWebhook(webhookCreateParams{
		URL:    target.URL,
		Events: []string{webhookEventTypeTest},
		Retry: &webhookRetryPolicy{
			MaxAttempts:       3,
			InitialBackoffMS:  100,
			MaxBackoffMS:      1000,
			BackoffMultiplier: 2,
			JitterFactor:      0,
			TimeoutMS:         5000,
		},
	})
	if err != nil {
		t.Fatalf("createWebhook failed: %v", err)
	}
	envelope, err := newWebhookEnvelope(webhookEventTypeTest, map[string]any{"ok": true}, "manual_test")
	if err != nil {
		t.Fatalf("newWebhookEnvelope failed: %v", err)
	}
	deliveries, err := manager.enqueueEventForWebhook(webhook.ID, webhookEventTypeTest, envelope)
	if err != nil {
		t.Fatalf("enqueueEventForWebhook failed: %v", err)
	}
	deliveryID := deliveries[0].ID

	manager.executeDelivery(deliveryID)
	first, ok := manager.getDelivery(deliveryID)
	if !ok {
		t.Fatalf("delivery %s missing after first attempt", deliveryID)
	}
	if first.Status != webhookStatusRetryScheduled {
		t.Fatalf("expected retry_scheduled after first failure, got %s", first.Status)
	}
	if first.AttemptCount != 1 {
		t.Fatalf("expected attempt count 1 after first send, got %d", first.AttemptCount)
	}

	manager.executeDelivery(deliveryID)
	second, ok := manager.getDelivery(deliveryID)
	if !ok {
		t.Fatalf("delivery %s missing after second attempt", deliveryID)
	}
	if second.Status != webhookStatusSucceeded {
		t.Fatalf("expected succeeded after second send, got %s", second.Status)
	}
	if second.AttemptCount != 2 {
		t.Fatalf("expected attempt count 2 after success, got %d", second.AttemptCount)
	}

	mu.Lock()
	defer mu.Unlock()
	if requestCount != 2 {
		t.Fatalf("expected 2 HTTP requests, got %d", requestCount)
	}
	if got := lastHeaders.Get("webhook-id"); got == "" {
		t.Fatal("expected webhook-id header")
	}
	if got := lastHeaders.Get("webhook-signature"); got == "" {
		t.Fatal("expected webhook-signature header")
	}
	if got := lastHeaders.Get("X-EasyMatrix-Signature"); got == "" {
		t.Fatal("expected X-EasyMatrix-Signature header")
	}
}

func TestWebhookDeliveryAutoDisableAfterFailures(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer target.Close()

	manager := newWebhookManager(&Server{}, filepath.Join(t.TempDir(), "state.json"))
	webhook, _, err := manager.createWebhook(webhookCreateParams{
		URL:    target.URL,
		Events: []string{webhookEventTypeTest},
		Retry: &webhookRetryPolicy{
			MaxAttempts:              5,
			InitialBackoffMS:         100,
			MaxBackoffMS:             500,
			BackoffMultiplier:        2,
			JitterFactor:             0,
			TimeoutMS:                5000,
			AutoDisableAfterFailures: 2,
		},
	})
	if err != nil {
		t.Fatalf("createWebhook failed: %v", err)
	}
	envelope, err := newWebhookEnvelope(webhookEventTypeTest, map[string]any{"ok": true}, "manual_test")
	if err != nil {
		t.Fatalf("newWebhookEnvelope failed: %v", err)
	}
	deliveries, err := manager.enqueueEventForWebhook(webhook.ID, webhookEventTypeTest, envelope)
	if err != nil {
		t.Fatalf("enqueueEventForWebhook failed: %v", err)
	}
	deliveryID := deliveries[0].ID

	manager.executeDelivery(deliveryID)
	manager.executeDelivery(deliveryID)

	delivery, ok := manager.getDelivery(deliveryID)
	if !ok {
		t.Fatalf("delivery %s not found", deliveryID)
	}
	if delivery.Status != webhookStatusFailed {
		t.Fatalf("expected failed status after auto-disable threshold, got %s", delivery.Status)
	}
	updatedWebhook, ok := manager.getWebhook(webhook.ID)
	if !ok {
		t.Fatalf("webhook %s not found", webhook.ID)
	}
	if updatedWebhook.Enabled {
		t.Fatal("expected webhook to be auto-disabled")
	}
	if updatedWebhook.DisabledReason == "" {
		t.Fatal("expected disabled reason to be set")
	}
}
