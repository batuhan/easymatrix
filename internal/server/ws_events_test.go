package server

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWSProcessRawPayloadRejectsWildcardWithSpecificIDs(t *testing.T) {
	hub, messages := newTestWSHub()

	err := hub.processRawPayload(1, []byte(`{"type":"subscriptions.set","requestID":"r1","chatIDs":["*","chat_a"]}`))
	if err != nil {
		t.Fatalf("processRawPayload returned error: %v", err)
	}
	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}

	msg := decodeWSErrorMessage(t, (*messages)[0])
	if msg.Code != wsErrorCodeInvalidPayload {
		t.Fatalf("expected invalid payload error, got %q", msg.Code)
	}
	if msg.RequestID != "r1" {
		t.Fatalf("expected requestID to round-trip, got %q", msg.RequestID)
	}
}

func TestWSProcessRawPayloadRejectsUnknownCommands(t *testing.T) {
	hub, messages := newTestWSHub()

	err := hub.processRawPayload(1, []byte(`{"type":"subscriptions.get","requestID":"r2"}`))
	if err != nil {
		t.Fatalf("processRawPayload returned error: %v", err)
	}
	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}

	msg := decodeWSErrorMessage(t, (*messages)[0])
	if msg.Code != wsErrorCodeInvalidCommand {
		t.Fatalf("expected invalid command error, got %q", msg.Code)
	}
	if msg.RequestID != "r2" {
		t.Fatalf("expected requestID to round-trip, got %q", msg.RequestID)
	}
}

func TestBuildWSFingerprintIgnoresTimestampLikeFields(t *testing.T) {
	domainEvent := wsDomainEvent{
		Type:   wsDomainTypeMessageUpserted,
		ChatID: "chat1",
		IDs:    []string{"m1"},
	}

	first := buildWSFingerprint(domainEvent, []compatRecord{{
		"id":        "m1",
		"chatID":    "chat1",
		"timestamp": "2026-01-01T00:00:00.000Z",
		"ts":        100,
		"text":      "same",
	}})
	second := buildWSFingerprint(domainEvent, []compatRecord{{
		"id":        "m1",
		"chatID":    "chat1",
		"timestamp": "2026-01-01T00:00:01.000Z",
		"ts":        200,
		"text":      "same",
	}})

	if first != second {
		t.Fatalf("expected equivalent fingerprints, got %q and %q", first, second)
	}
}

func TestWSDropDuplicateUsesDebounceWindow(t *testing.T) {
	hub := &wsHub{
		recentFingerprints: make(map[string]time.Time),
	}
	domainEvent := wsDomainEvent{
		Type:   wsDomainTypeChatUpserted,
		ChatID: "chat1",
		IDs:    []string{"chat1"},
	}

	now := time.Now().UTC()
	if hub.dropDuplicate(domainEvent, nil, now) {
		t.Fatalf("first event should never be dropped")
	}
	if !hub.dropDuplicate(domainEvent, nil, now.Add(100*time.Millisecond)) {
		t.Fatalf("expected duplicate event inside debounce window to be dropped")
	}
	if hub.dropDuplicate(domainEvent, nil, now.Add(2*wsDuplicateEventDebounce)) {
		t.Fatalf("expected event outside debounce window to be delivered")
	}
}

func newTestWSHub() (*wsHub, *[]any) {
	messages := make([]any, 0, 1)
	hub := &wsHub{
		clients:            make(map[uint64]*wsClient),
		recentFingerprints: make(map[string]time.Time),
	}
	hub.clients[1] = &wsClient{
		id: 1,
		state: &wsClientState{
			chatIDs: []string{},
		},
		send: func(payload any) error {
			messages = append(messages, payload)
			return nil
		},
	}
	return hub, &messages
}

func decodeWSErrorMessage(t *testing.T, payload any) wsErrorMessage {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal websocket payload: %v", err)
	}

	var decoded wsErrorMessage
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal websocket payload: %v", err)
	}
	return decoded
}
