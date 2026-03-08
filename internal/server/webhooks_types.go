package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/gomuks/pkg/hicli/jsoncmd"
)

const (
	webhookStateVersion = 1

	webhookStatusPending        = "pending"
	webhookStatusDelivering     = "delivering"
	webhookStatusRetryScheduled = "retry_scheduled"
	webhookStatusSucceeded      = "succeeded"
	webhookStatusFailed         = "failed"
	webhookStatusCancelled      = "cancelled"

	webhookEventTypeWildcard       = "*"
	webhookEventTypeChatUpserted   = "chat.upserted"
	webhookEventTypeChatDeleted    = "chat.deleted"
	webhookEventTypeMessageUpsert  = "message.upserted"
	webhookEventTypeMessageDeleted = "message.deleted"
	webhookEventTypeTest           = "test.event"
)

var supportedWebhookEventTypes = []string{
	webhookEventTypeChatUpserted,
	webhookEventTypeChatDeleted,
	webhookEventTypeMessageUpsert,
	webhookEventTypeMessageDeleted,
	webhookEventTypeTest,
}

type webhookRetryPolicy struct {
	MaxAttempts              int     `json:"maxAttempts"`
	InitialBackoffMS         int     `json:"initialBackoffMs"`
	MaxBackoffMS             int     `json:"maxBackoffMs"`
	BackoffMultiplier        float64 `json:"backoffMultiplier"`
	JitterFactor             float64 `json:"jitterFactor"`
	TimeoutMS                int     `json:"timeoutMs"`
	AutoDisableAfterFailures int     `json:"autoDisableAfterFailures,omitempty"`
}

type webhookEndpoint struct {
	ID                  string             `json:"id"`
	URL                 string             `json:"url"`
	Events              []string           `json:"events"`
	Enabled             bool               `json:"enabled"`
	Secret              string             `json:"secret"`
	Headers             map[string]string  `json:"headers,omitempty"`
	Retry               webhookRetryPolicy `json:"retry"`
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
	LastSuccessAt       *time.Time         `json:"lastSuccessAt,omitempty"`
	LastFailureAt       *time.Time         `json:"lastFailureAt,omitempty"`
	ConsecutiveFailures int                `json:"consecutiveFailures"`
	DisabledReason      string             `json:"disabledReason,omitempty"`
}

type webhookDeliveryAttempt struct {
	Attempt        int       `json:"attempt"`
	StartedAt      time.Time `json:"startedAt"`
	DurationMS     int64     `json:"durationMs"`
	ResponseStatus int       `json:"responseStatus,omitempty"`
	Error          string    `json:"error,omitempty"`
	Retryable      bool      `json:"retryable"`
}

type webhookDelivery struct {
	ID             string                   `json:"id"`
	WebhookID      string                   `json:"webhookID"`
	EventID        string                   `json:"eventID"`
	EventType      string                   `json:"eventType"`
	Payload        json.RawMessage          `json:"payload"`
	Status         string                   `json:"status"`
	AttemptCount   int                      `json:"attemptCount"`
	NextAttemptAt  *time.Time               `json:"nextAttemptAt,omitempty"`
	LastAttemptAt  *time.Time               `json:"lastAttemptAt,omitempty"`
	LastError      string                   `json:"lastError,omitempty"`
	ResponseStatus int                      `json:"responseStatus,omitempty"`
	Attempts       []webhookDeliveryAttempt `json:"attempts,omitempty"`
	CreatedAt      time.Time                `json:"createdAt"`
	UpdatedAt      time.Time                `json:"updatedAt"`
}

type webhookEventRequest struct {
	ID     string `json:"id,omitempty"`
	Source string `json:"source,omitempty"`
}

type webhookEventData struct {
	Object             any            `json:"object"`
	PreviousAttributes map[string]any `json:"previous_attributes,omitempty"`
}

type webhookEventEnvelope struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Created  int64                `json:"created"`
	Livemode bool                 `json:"livemode"`
	Data     webhookEventData     `json:"data"`
	Request  *webhookEventRequest `json:"request,omitempty"`
}

type webhookPersistedState struct {
	Version    int                        `json:"version"`
	Webhooks   map[string]webhookEndpoint `json:"webhooks"`
	Deliveries map[string]webhookDelivery `json:"deliveries"`
}

type webhookManager struct {
	server    *Server
	statePath string

	mu sync.RWMutex

	webhooks   map[string]webhookEndpoint
	deliveries map[string]webhookDelivery

	wakeCh      chan struct{}
	eventCh     chan *jsoncmd.SyncComplete
	stopCh      chan struct{}
	stoppedCh   chan struct{}
	deliverySem chan struct{}
	unsubscribe func()
}

type webhookView struct {
	ID                  string             `json:"id"`
	URL                 string             `json:"url"`
	Events              []string           `json:"events"`
	Enabled             bool               `json:"enabled"`
	Headers             map[string]string  `json:"headers,omitempty"`
	Retry               webhookRetryPolicy `json:"retry"`
	HasSecret           bool               `json:"hasSecret"`
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
	LastSuccessAt       *time.Time         `json:"lastSuccessAt,omitempty"`
	LastFailureAt       *time.Time         `json:"lastFailureAt,omitempty"`
	ConsecutiveFailures int                `json:"consecutiveFailures"`
	DisabledReason      string             `json:"disabledReason,omitempty"`
}

type webhookDeliveryView struct {
	ID             string                   `json:"id"`
	WebhookID      string                   `json:"webhookID"`
	EventID        string                   `json:"eventID"`
	EventType      string                   `json:"eventType"`
	Status         string                   `json:"status"`
	AttemptCount   int                      `json:"attemptCount"`
	NextAttemptAt  *time.Time               `json:"nextAttemptAt,omitempty"`
	LastAttemptAt  *time.Time               `json:"lastAttemptAt,omitempty"`
	LastError      string                   `json:"lastError,omitempty"`
	ResponseStatus int                      `json:"responseStatus,omitempty"`
	Attempts       []webhookDeliveryAttempt `json:"attempts,omitempty"`
	CreatedAt      time.Time                `json:"createdAt"`
	UpdatedAt      time.Time                `json:"updatedAt"`
}

func defaultWebhookRetryPolicy() webhookRetryPolicy {
	return webhookRetryPolicy{
		MaxAttempts:              8,
		InitialBackoffMS:         5000,
		MaxBackoffMS:             10 * 60 * 1000,
		BackoffMultiplier:        2.0,
		JitterFactor:             0.25,
		TimeoutMS:                15000,
		AutoDisableAfterFailures: 0,
	}
}

func normalizeWebhookRetryPolicy(input *webhookRetryPolicy) (webhookRetryPolicy, error) {
	policy := defaultWebhookRetryPolicy()
	if input == nil {
		return policy, nil
	}

	if input.MaxAttempts != 0 {
		policy.MaxAttempts = input.MaxAttempts
	}
	if input.InitialBackoffMS != 0 {
		policy.InitialBackoffMS = input.InitialBackoffMS
	}
	if input.MaxBackoffMS != 0 {
		policy.MaxBackoffMS = input.MaxBackoffMS
	}
	if input.BackoffMultiplier != 0 {
		policy.BackoffMultiplier = input.BackoffMultiplier
	}
	if input.JitterFactor != 0 {
		policy.JitterFactor = input.JitterFactor
	}
	if input.TimeoutMS != 0 {
		policy.TimeoutMS = input.TimeoutMS
	}
	if input.AutoDisableAfterFailures != 0 {
		policy.AutoDisableAfterFailures = input.AutoDisableAfterFailures
	}

	if policy.MaxAttempts < 1 || policy.MaxAttempts > 50 {
		return webhookRetryPolicy{}, fmt.Errorf("retry.maxAttempts must be between 1 and 50")
	}
	if policy.InitialBackoffMS < 100 || policy.InitialBackoffMS > 3600000 {
		return webhookRetryPolicy{}, fmt.Errorf("retry.initialBackoffMs must be between 100 and 3600000")
	}
	if policy.MaxBackoffMS < policy.InitialBackoffMS || policy.MaxBackoffMS > 24*3600000 {
		return webhookRetryPolicy{}, fmt.Errorf("retry.maxBackoffMs must be >= initialBackoffMs and <= 86400000")
	}
	if policy.BackoffMultiplier < 1 || policy.BackoffMultiplier > 10 {
		return webhookRetryPolicy{}, fmt.Errorf("retry.backoffMultiplier must be between 1 and 10")
	}
	if policy.JitterFactor < 0 || policy.JitterFactor > 1 {
		return webhookRetryPolicy{}, fmt.Errorf("retry.jitterFactor must be between 0 and 1")
	}
	if policy.TimeoutMS < 1000 || policy.TimeoutMS > 120000 {
		return webhookRetryPolicy{}, fmt.Errorf("retry.timeoutMs must be between 1000 and 120000")
	}
	if policy.AutoDisableAfterFailures < 0 || policy.AutoDisableAfterFailures > 1000 {
		return webhookRetryPolicy{}, fmt.Errorf("retry.autoDisableAfterFailures must be between 0 and 1000")
	}

	return policy, nil
}

func normalizeWebhookEvents(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{webhookEventTypeWildcard}, nil
	}

	seen := make(map[string]struct{}, len(raw))
	events := make([]string, 0, len(raw))
	hasWildcard := false
	for _, item := range raw {
		eventType := strings.TrimSpace(item)
		if eventType == "" {
			continue
		}
		if eventType == webhookEventTypeWildcard {
			hasWildcard = true
		} else if !isSupportedWebhookEventType(eventType) {
			return nil, fmt.Errorf("unsupported event type: %s", eventType)
		}
		if _, ok := seen[eventType]; ok {
			continue
		}
		seen[eventType] = struct{}{}
		events = append(events, eventType)
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("events must include at least one event type")
	}
	if hasWildcard && len(events) > 1 {
		return nil, fmt.Errorf("events cannot combine '*' with specific event types")
	}
	sort.Strings(events)
	return events, nil
}

func isSupportedWebhookEventType(eventType string) bool {
	for _, candidate := range supportedWebhookEventTypes {
		if eventType == candidate {
			return true
		}
	}
	return false
}

func validateWebhookURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("url must use http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("url host is required")
	}
	return parsed.String(), nil
}

func sanitizeWebhookHeaders(raw map[string]string) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	output := make(map[string]string, len(raw))
	for key, value := range raw {
		key = strings.TrimSpace(http.CanonicalHeaderKey(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		output[key] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func (endpoint webhookEndpoint) matchesEvent(eventType string) bool {
	if !endpoint.Enabled {
		return false
	}
	for _, candidate := range endpoint.Events {
		if candidate == webhookEventTypeWildcard || candidate == eventType {
			return true
		}
	}
	return false
}

func (endpoint webhookEndpoint) toView() webhookView {
	events := append([]string(nil), endpoint.Events...)
	headers := make(map[string]string, len(endpoint.Headers))
	for key, value := range endpoint.Headers {
		headers[key] = value
	}
	if len(headers) == 0 {
		headers = nil
	}
	return webhookView{
		ID:                  endpoint.ID,
		URL:                 endpoint.URL,
		Events:              events,
		Enabled:             endpoint.Enabled,
		Headers:             headers,
		Retry:               endpoint.Retry,
		HasSecret:           strings.TrimSpace(endpoint.Secret) != "",
		CreatedAt:           endpoint.CreatedAt,
		UpdatedAt:           endpoint.UpdatedAt,
		LastSuccessAt:       endpoint.LastSuccessAt,
		LastFailureAt:       endpoint.LastFailureAt,
		ConsecutiveFailures: endpoint.ConsecutiveFailures,
		DisabledReason:      endpoint.DisabledReason,
	}
}

func (delivery webhookDelivery) toView(includeAttempts bool) webhookDeliveryView {
	view := webhookDeliveryView{
		ID:             delivery.ID,
		WebhookID:      delivery.WebhookID,
		EventID:        delivery.EventID,
		EventType:      delivery.EventType,
		Status:         delivery.Status,
		AttemptCount:   delivery.AttemptCount,
		NextAttemptAt:  delivery.NextAttemptAt,
		LastAttemptAt:  delivery.LastAttemptAt,
		LastError:      delivery.LastError,
		ResponseStatus: delivery.ResponseStatus,
		CreatedAt:      delivery.CreatedAt,
		UpdatedAt:      delivery.UpdatedAt,
	}
	if includeAttempts {
		view.Attempts = append([]webhookDeliveryAttempt(nil), delivery.Attempts...)
	}
	return view
}

func isWebhookDeliveryTerminal(status string) bool {
	switch status {
	case webhookStatusSucceeded, webhookStatusFailed, webhookStatusCancelled:
		return true
	default:
		return false
	}
}

func buildWebhookSignatures(messageID, timestamp string, payload []byte, secret string) (string, string) {
	canonical := messageID + "." + timestamp + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	sum := mac.Sum(nil)
	standard := "v1," + base64.StdEncoding.EncodeToString(sum)
	legacy := "sha256=" + hex.EncodeToString(sum)
	return standard, legacy
}
