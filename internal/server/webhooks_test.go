package server

import (
	"path/filepath"
	"testing"
)

func TestNormalizeWebhookEventsRejectsWildcardMix(t *testing.T) {
	_, err := normalizeWebhookEvents([]string{"*", webhookEventTypeChatUpserted})
	if err == nil {
		t.Fatal("expected wildcard mix to fail validation")
	}
}

func TestBuildWebhookSignaturesDeterministic(t *testing.T) {
	payload := []byte(`{"hello":"world"}`)
	stdOne, legacyOne := buildWebhookSignatures("evt_1", "1730000000", payload, "secret-key")
	stdTwo, legacyTwo := buildWebhookSignatures("evt_1", "1730000000", payload, "secret-key")

	if stdOne == "" || legacyOne == "" {
		t.Fatal("expected non-empty signatures")
	}
	if stdOne != stdTwo || legacyOne != legacyTwo {
		t.Fatalf("expected deterministic signatures, got %q/%q and %q/%q", stdOne, legacyOne, stdTwo, legacyTwo)
	}
}

func TestNextWebhookRetryDelayBounded(t *testing.T) {
	policy := webhookRetryPolicy{
		MaxAttempts:       10,
		InitialBackoffMS:  100,
		MaxBackoffMS:      1000,
		BackoffMultiplier: 2,
		JitterFactor:      0,
		TimeoutMS:         15000,
	}
	first := nextWebhookRetryDelay(policy, 1)
	second := nextWebhookRetryDelay(policy, 2)
	third := nextWebhookRetryDelay(policy, 3)
	last := nextWebhookRetryDelay(policy, 15)

	if second <= first {
		t.Fatalf("expected second delay > first delay, got %s <= %s", second, first)
	}
	if third <= second {
		t.Fatalf("expected third delay > second delay, got %s <= %s", third, second)
	}
	if last > 1000*1000*1000 {
		t.Fatalf("expected delay to be bounded to max interval, got %s", last)
	}
}

func TestWebhookStatePersistsAndLoads(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "webhooks", "state.json")
	manager := newWebhookManager(&Server{}, statePath)

	webhook, _, err := manager.createWebhook(webhookCreateParams{
		URL:    "https://example.com/webhook",
		Events: []string{webhookEventTypeWildcard},
	})
	if err != nil {
		t.Fatalf("createWebhook failed: %v", err)
	}
	envelope, err := newWebhookEnvelope(webhookEventTypeTest, map[string]any{"message": "hello"}, "manual_test")
	if err != nil {
		t.Fatalf("newWebhookEnvelope failed: %v", err)
	}
	deliveries, err := manager.enqueueEventForWebhook(webhook.ID, webhookEventTypeTest, envelope)
	if err != nil {
		t.Fatalf("enqueueEventForWebhook failed: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("expected one delivery, got %d", len(deliveries))
	}

	reloaded := newWebhookManager(&Server{}, statePath)
	if err = reloaded.loadState(); err != nil {
		t.Fatalf("loadState failed: %v", err)
	}
	if _, ok := reloaded.getWebhook(webhook.ID); !ok {
		t.Fatalf("expected webhook %s to be restored from state", webhook.ID)
	}
	if _, ok := reloaded.getDelivery(deliveries[0].ID); !ok {
		t.Fatalf("expected delivery %s to be restored from state", deliveries[0].ID)
	}
}
