package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	errs "github.com/batuhan/easymatrix/internal/errors"
)

type webhookListResponse struct {
	Items      []webhookView `json:"items"`
	HasMore    bool          `json:"hasMore"`
	NextCursor *string       `json:"nextCursor,omitempty"`
}

type webhookCreateResponse struct {
	Webhook       webhookView `json:"webhook"`
	SigningSecret string      `json:"signingSecret,omitempty"`
}

type webhookDeliveryListResponse struct {
	Items      []webhookDeliveryView `json:"items"`
	HasMore    bool                  `json:"hasMore"`
	NextCursor *string               `json:"nextCursor,omitempty"`
}

type webhookDeliveryDetails struct {
	Delivery webhookDeliveryView `json:"delivery"`
	Payload  any                 `json:"payload,omitempty"`
}

func (s *Server) webhookManagerOrError() (*webhookManager, error) {
	if s.webhooks == nil {
		return nil, errs.Internal(fmt.Errorf("webhook manager is not initialized"))
	}
	return s.webhooks, nil
}

func (s *Server) listWebhookEvents(w http.ResponseWriter, r *http.Request) error {
	events := append([]string{webhookEventTypeWildcard}, supportedWebhookEventTypes...)
	return writeJSON(w, map[string]any{
		"items": events,
	})
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) error {
	manager, err := s.webhookManagerOrError()
	if err != nil {
		return err
	}

	var req struct {
		URL     string              `json:"url"`
		Events  []string            `json:"events"`
		Enabled *bool               `json:"enabled,omitempty"`
		Secret  string              `json:"secret,omitempty"`
		Headers map[string]string   `json:"headers,omitempty"`
		Retry   *webhookRetryPolicy `json:"retry,omitempty"`
	}
	if err = decodeJSON(r, &req); err != nil {
		return err
	}

	webhook, generatedSecret, err := manager.createWebhook(webhookCreateParams{
		URL:     req.URL,
		Events:  req.Events,
		Enabled: req.Enabled,
		Secret:  req.Secret,
		Headers: req.Headers,
		Retry:   req.Retry,
	})
	if err != nil {
		return errs.Validation(map[string]any{"error": err.Error()})
	}

	w.WriteHeader(http.StatusCreated)
	return writeJSON(w, webhookCreateResponse{
		Webhook:       webhook.toView(),
		SigningSecret: generatedSecret,
	})
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) error {
	manager, err := s.webhookManagerOrError()
	if err != nil {
		return err
	}
	limit := parseListLimit(r.URL.Query().Get("limit"))
	offset := decodeListCursor(r.URL.Query().Get("cursor"))

	items, hasMore, nextOffset := manager.listWebhooks(limit, offset)
	views := make([]webhookView, 0, len(items))
	for _, webhook := range items {
		views = append(views, webhook.toView())
	}
	var nextCursor *string
	if hasMore {
		raw := strconv.Itoa(nextOffset)
		nextCursor = &raw
	}
	return writeJSON(w, webhookListResponse{
		Items:      views,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func (s *Server) getWebhook(w http.ResponseWriter, r *http.Request) error {
	manager, err := s.webhookManagerOrError()
	if err != nil {
		return err
	}
	webhookID := strings.TrimSpace(r.PathValue("webhookID"))
	if webhookID == "" {
		return errs.Validation(map[string]any{"webhookID": "webhookID is required"})
	}
	webhook, ok := manager.getWebhook(webhookID)
	if !ok {
		return errs.NotFound("Webhook not found")
	}
	return writeJSON(w, webhook.toView())
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request) error {
	manager, err := s.webhookManagerOrError()
	if err != nil {
		return err
	}
	webhookID := strings.TrimSpace(r.PathValue("webhookID"))
	if webhookID == "" {
		return errs.Validation(map[string]any{"webhookID": "webhookID is required"})
	}
	var req struct {
		URL          *string             `json:"url,omitempty"`
		Events       []string            `json:"events,omitempty"`
		Enabled      *bool               `json:"enabled,omitempty"`
		Secret       *string             `json:"secret,omitempty"`
		RotateSecret bool                `json:"rotateSecret,omitempty"`
		Headers      map[string]string   `json:"headers,omitempty"`
		Retry        *webhookRetryPolicy `json:"retry,omitempty"`
	}
	if err = decodeJSON(r, &req); err != nil {
		return err
	}

	webhook, rotatedSecret, err := manager.updateWebhook(webhookID, webhookUpdateParams{
		URL:          req.URL,
		Events:       req.Events,
		Enabled:      req.Enabled,
		Secret:       req.Secret,
		RotateSecret: req.RotateSecret,
		Headers:      req.Headers,
		Retry:        req.Retry,
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errs.NotFound("Webhook not found")
		}
		return errs.Validation(map[string]any{"error": err.Error()})
	}
	return writeJSON(w, webhookCreateResponse{
		Webhook:       webhook.toView(),
		SigningSecret: rotatedSecret,
	})
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) error {
	manager, err := s.webhookManagerOrError()
	if err != nil {
		return err
	}
	webhookID := strings.TrimSpace(r.PathValue("webhookID"))
	if webhookID == "" {
		return errs.Validation(map[string]any{"webhookID": "webhookID is required"})
	}
	deleted, err := manager.deleteWebhook(webhookID)
	if err != nil {
		return errs.Internal(err)
	}
	if !deleted {
		return errs.NotFound("Webhook not found")
	}
	return writeJSON(w, map[string]any{"success": true})
}

func (s *Server) listWebhookDeliveries(w http.ResponseWriter, r *http.Request) error {
	manager, err := s.webhookManagerOrError()
	if err != nil {
		return err
	}
	webhookID := strings.TrimSpace(r.PathValue("webhookID"))
	if webhookID == "" {
		return errs.Validation(map[string]any{"webhookID": "webhookID is required"})
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := parseListLimit(r.URL.Query().Get("limit"))
	offset := decodeListCursor(r.URL.Query().Get("cursor"))

	items, hasMore, nextOffset, err := manager.listDeliveries(webhookID, status, limit, offset)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errs.NotFound("Webhook not found")
		}
		return errs.Internal(err)
	}
	views := make([]webhookDeliveryView, 0, len(items))
	for _, delivery := range items {
		views = append(views, delivery.toView(false))
	}
	var nextCursor *string
	if hasMore {
		raw := strconv.Itoa(nextOffset)
		nextCursor = &raw
	}
	return writeJSON(w, webhookDeliveryListResponse{
		Items:      views,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func (s *Server) getWebhookDelivery(w http.ResponseWriter, r *http.Request) error {
	manager, err := s.webhookManagerOrError()
	if err != nil {
		return err
	}
	deliveryID := strings.TrimSpace(r.PathValue("deliveryID"))
	if deliveryID == "" {
		return errs.Validation(map[string]any{"deliveryID": "deliveryID is required"})
	}
	delivery, ok := manager.getDelivery(deliveryID)
	if !ok {
		return errs.NotFound("Delivery not found")
	}
	var payload any
	if unmarshalErr := json.Unmarshal(delivery.Payload, &payload); unmarshalErr != nil {
		payload = json.RawMessage(delivery.Payload)
	}
	return writeJSON(w, webhookDeliveryDetails{
		Delivery: delivery.toView(true),
		Payload:  payload,
	})
}

func (s *Server) retryWebhookDelivery(w http.ResponseWriter, r *http.Request) error {
	manager, err := s.webhookManagerOrError()
	if err != nil {
		return err
	}
	deliveryID := strings.TrimSpace(r.PathValue("deliveryID"))
	if deliveryID == "" {
		return errs.Validation(map[string]any{"deliveryID": "deliveryID is required"})
	}
	delivery, err := manager.retryDelivery(deliveryID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errs.NotFound("Delivery not found")
		}
		if strings.Contains(strings.ToLower(err.Error()), "currently in progress") {
			return errs.New(http.StatusConflict, "CONFLICT", "Delivery is currently in progress", nil)
		}
		return errs.Validation(map[string]any{"error": err.Error()})
	}
	return writeJSON(w, map[string]any{
		"delivery": delivery.toView(true),
	})
}

func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) error {
	manager, err := s.webhookManagerOrError()
	if err != nil {
		return err
	}
	webhookID := strings.TrimSpace(r.PathValue("webhookID"))
	if webhookID == "" {
		return errs.Validation(map[string]any{"webhookID": "webhookID is required"})
	}
	var req struct {
		EventType string `json:"eventType,omitempty"`
		Data      any    `json:"data,omitempty"`
	}
	if err = decodeOptionalJSON(r, &req); err != nil {
		return err
	}
	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" {
		eventType = webhookEventTypeTest
	}
	if !isSupportedWebhookEventType(eventType) {
		return errs.Validation(map[string]any{"eventType": "unsupported event type"})
	}
	testPayload := req.Data
	if testPayload == nil {
		testPayload = map[string]any{
			"message": "test webhook delivery",
		}
	}
	envelope, err := newWebhookEnvelope(eventType, testPayload, "manual_test")
	if err != nil {
		return errs.Internal(err)
	}
	deliveries, err := manager.enqueueEventForWebhook(webhookID, eventType, envelope)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errs.NotFound("Webhook not found")
		}
		return errs.Validation(map[string]any{"error": err.Error()})
	}
	items := make([]webhookDeliveryView, 0, len(deliveries))
	for _, delivery := range deliveries {
		items = append(items, delivery.toView(true))
	}
	return writeJSON(w, map[string]any{
		"items": items,
	})
}

func parseListLimit(raw string) int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return webhookDefaultListLimit
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value <= 0 {
		return webhookDefaultListLimit
	}
	if value > webhookMaxListLimit {
		return webhookMaxListLimit
	}
	return value
}
