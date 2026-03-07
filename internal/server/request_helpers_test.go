package server

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestDecodeOptionalJSONAcceptsEmptyBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/focus", bytes.NewBuffer(nil))
	var payload struct {
		Name string `json:"name"`
	}
	if err := decodeOptionalJSON(req, &payload); err != nil {
		t.Fatalf("decodeOptionalJSON returned error for empty body: %v", err)
	}
}

func TestDecodeOptionalJSONRejectsMalformedBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/focus", bytes.NewBufferString(`{"name":`))
	var payload struct {
		Name string `json:"name"`
	}
	if err := decodeOptionalJSON(req, &payload); err == nil {
		t.Fatal("decodeOptionalJSON accepted malformed JSON")
	}
}

func TestReadMessageIDPrefersPathOverBodyAndQuery(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/v1/chats/chat/messages/query-id?messageID=query-id", nil)
	req.SetPathValue("messageID", "path-id")
	if messageID := readMessageID(req, "body-id"); messageID != "path-id" {
		t.Fatalf("expected path-id, got %q", messageID)
	}
}
