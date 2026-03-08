package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/gomuks/pkg/hicli/jsoncmd"
)

const (
	webhookSchedulerTick          = 1 * time.Second
	webhookDefaultListLimit       = 50
	webhookMaxListLimit           = 200
	webhookTerminalRetention      = 7 * 24 * time.Hour
	webhookMaxPersistedDeliveries = 5000
	webhookMaxPersistedAttempts   = 20
	webhookDefaultConcurrentSends = 8
)

type webhookCreateParams struct {
	URL     string
	Events  []string
	Enabled *bool
	Secret  string
	Headers map[string]string
	Retry   *webhookRetryPolicy
}

type webhookUpdateParams struct {
	URL          *string
	Events       []string
	Enabled      *bool
	Secret       *string
	RotateSecret bool
	Headers      map[string]string
	Retry        *webhookRetryPolicy
}

func newWebhookManager(server *Server, statePath string) *webhookManager {
	return &webhookManager{
		server:      server,
		statePath:   statePath,
		webhooks:    make(map[string]webhookEndpoint),
		deliveries:  make(map[string]webhookDelivery),
		wakeCh:      make(chan struct{}, 1),
		eventCh:     make(chan *jsoncmd.SyncComplete, 256),
		stopCh:      make(chan struct{}),
		stoppedCh:   make(chan struct{}),
		deliverySem: make(chan struct{}, webhookDefaultConcurrentSends),
	}
}

func (m *webhookManager) start() error {
	if m == nil {
		return nil
	}
	if err := m.loadState(); err != nil {
		return err
	}
	go m.runEventLoop()
	go m.runScheduler()
	return m.subscribeRuntimeEvents()
}

func (m *webhookManager) stop() {
	if m == nil {
		return
	}
	select {
	case <-m.stopCh:
		return
	default:
		close(m.stopCh)
	}
	if m.unsubscribe != nil {
		m.unsubscribe()
	}
	<-m.stoppedCh
}

func (m *webhookManager) signalScheduler() {
	select {
	case m.wakeCh <- struct{}{}:
	default:
	}
}

func (m *webhookManager) loadState() error {
	if strings.TrimSpace(m.statePath) == "" {
		return nil
	}
	raw, err := os.ReadFile(m.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read webhook state: %w", err)
	}
	var persisted webhookPersistedState
	if err = json.Unmarshal(raw, &persisted); err != nil {
		return fmt.Errorf("failed to parse webhook state: %w", err)
	}
	if persisted.Version != webhookStateVersion {
		return fmt.Errorf("unsupported webhook state version: %d", persisted.Version)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for id, webhook := range persisted.Webhooks {
		if strings.TrimSpace(id) == "" {
			continue
		}
		normalizedURL, urlErr := validateWebhookURL(webhook.URL)
		if urlErr != nil {
			continue
		}
		events, eventErr := normalizeWebhookEvents(webhook.Events)
		if eventErr != nil {
			continue
		}
		retryPolicy, retryErr := normalizeWebhookRetryPolicy(&webhook.Retry)
		if retryErr != nil {
			continue
		}
		webhook.ID = id
		webhook.URL = normalizedURL
		webhook.Events = events
		webhook.Retry = retryPolicy
		webhook.Headers = sanitizeWebhookHeaders(webhook.Headers)
		if webhook.CreatedAt.IsZero() {
			webhook.CreatedAt = now
		}
		if webhook.UpdatedAt.IsZero() {
			webhook.UpdatedAt = webhook.CreatedAt
		}
		m.webhooks[id] = webhook
	}

	for id, delivery := range persisted.Deliveries {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if strings.TrimSpace(delivery.WebhookID) == "" || strings.TrimSpace(delivery.EventID) == "" || strings.TrimSpace(delivery.EventType) == "" {
			continue
		}
		if delivery.Payload == nil || len(delivery.Payload) == 0 {
			continue
		}
		if _, ok := m.webhooks[delivery.WebhookID]; !ok {
			continue
		}
		if delivery.CreatedAt.IsZero() {
			delivery.CreatedAt = now
		}
		if delivery.UpdatedAt.IsZero() {
			delivery.UpdatedAt = delivery.CreatedAt
		}
		if delivery.Status == "" {
			delivery.Status = webhookStatusPending
		}
		if len(delivery.Attempts) > webhookMaxPersistedAttempts {
			delivery.Attempts = append([]webhookDeliveryAttempt(nil), delivery.Attempts[len(delivery.Attempts)-webhookMaxPersistedAttempts:]...)
		}
		m.deliveries[id] = delivery
	}
	m.pruneStateLocked(now)
	return nil
}

func (m *webhookManager) persistStateLocked(now time.Time) error {
	if strings.TrimSpace(m.statePath) == "" {
		return nil
	}
	m.pruneStateLocked(now)
	state := webhookPersistedState{
		Version:    webhookStateVersion,
		Webhooks:   make(map[string]webhookEndpoint, len(m.webhooks)),
		Deliveries: make(map[string]webhookDelivery, len(m.deliveries)),
	}
	for id, webhook := range m.webhooks {
		state.Webhooks[id] = webhook
	}
	for id, delivery := range m.deliveries {
		state.Deliveries[id] = delivery
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to encode webhook state: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(m.statePath), 0o700); err != nil {
		return fmt.Errorf("failed to create webhook state dir: %w", err)
	}
	if err = writeAtomicFile(m.statePath, raw, 0o600); err != nil {
		return fmt.Errorf("failed to persist webhook state: %w", err)
	}
	return nil
}

func (m *webhookManager) pruneStateLocked(now time.Time) {
	for id, delivery := range m.deliveries {
		if !isWebhookDeliveryTerminal(delivery.Status) {
			continue
		}
		if now.Sub(delivery.UpdatedAt) > webhookTerminalRetention {
			delete(m.deliveries, id)
		}
	}
	if len(m.deliveries) <= webhookMaxPersistedDeliveries {
		return
	}

	terminalIDs := make([]string, 0, len(m.deliveries))
	for id, delivery := range m.deliveries {
		if isWebhookDeliveryTerminal(delivery.Status) {
			terminalIDs = append(terminalIDs, id)
		}
	}
	sort.Slice(terminalIDs, func(i, j int) bool {
		left := m.deliveries[terminalIDs[i]]
		right := m.deliveries[terminalIDs[j]]
		return left.UpdatedAt.Before(right.UpdatedAt)
	})
	for _, id := range terminalIDs {
		if len(m.deliveries) <= webhookMaxPersistedDeliveries {
			break
		}
		delete(m.deliveries, id)
	}
}

func (m *webhookManager) createWebhook(params webhookCreateParams) (webhookEndpoint, string, error) {
	normalizedURL, err := validateWebhookURL(params.URL)
	if err != nil {
		return webhookEndpoint{}, "", err
	}
	events, err := normalizeWebhookEvents(params.Events)
	if err != nil {
		return webhookEndpoint{}, "", err
	}
	policy, err := normalizeWebhookRetryPolicy(params.Retry)
	if err != nil {
		return webhookEndpoint{}, "", err
	}

	secret := strings.TrimSpace(params.Secret)
	generatedSecret := ""
	if secret == "" {
		secretValue, tokenErr := randomHexToken(24)
		if tokenErr != nil {
			return webhookEndpoint{}, "", tokenErr
		}
		secret = secretValue
		generatedSecret = secretValue
	}

	webhookID, err := randomHexToken(12)
	if err != nil {
		return webhookEndpoint{}, "", err
	}
	now := time.Now().UTC()
	enabled := true
	if params.Enabled != nil {
		enabled = *params.Enabled
	}
	webhook := webhookEndpoint{
		ID:        webhookID,
		URL:       normalizedURL,
		Events:    events,
		Enabled:   enabled,
		Secret:    secret,
		Headers:   sanitizeWebhookHeaders(params.Headers),
		Retry:     policy,
		CreatedAt: now,
		UpdatedAt: now,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.webhooks[webhook.ID] = webhook
	if err = m.persistStateLocked(now); err != nil {
		delete(m.webhooks, webhook.ID)
		return webhookEndpoint{}, "", err
	}
	return webhook, generatedSecret, nil
}

func (m *webhookManager) listWebhooks(limit, offset int) ([]webhookEndpoint, bool, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return paginateWebhooksLocked(m.webhooks, limit, offset)
}

func paginateWebhooksLocked(webhooks map[string]webhookEndpoint, limit, offset int) ([]webhookEndpoint, bool, int) {
	if limit <= 0 {
		limit = webhookDefaultListLimit
	}
	if limit > webhookMaxListLimit {
		limit = webhookMaxListLimit
	}
	if offset < 0 {
		offset = 0
	}

	items := make([]webhookEndpoint, 0, len(webhooks))
	for _, item := range webhooks {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID > items[j].ID
	})
	if offset >= len(items) {
		return []webhookEndpoint{}, false, offset
	}
	end := offset + limit
	hasMore := end < len(items)
	if end > len(items) {
		end = len(items)
	}
	nextOffset := end
	return append([]webhookEndpoint(nil), items[offset:end]...), hasMore, nextOffset
}

func (m *webhookManager) getWebhook(webhookID string) (webhookEndpoint, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	webhook, ok := m.webhooks[webhookID]
	return webhook, ok
}

func (m *webhookManager) updateWebhook(webhookID string, params webhookUpdateParams) (webhookEndpoint, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	webhook, ok := m.webhooks[webhookID]
	if !ok {
		return webhookEndpoint{}, "", os.ErrNotExist
	}

	if params.URL != nil {
		normalizedURL, err := validateWebhookURL(*params.URL)
		if err != nil {
			return webhookEndpoint{}, "", err
		}
		webhook.URL = normalizedURL
	}
	if len(params.Events) > 0 {
		events, err := normalizeWebhookEvents(params.Events)
		if err != nil {
			return webhookEndpoint{}, "", err
		}
		webhook.Events = events
	}
	if params.Enabled != nil {
		webhook.Enabled = *params.Enabled
		if webhook.Enabled {
			webhook.DisabledReason = ""
		}
	}
	if params.Headers != nil {
		webhook.Headers = sanitizeWebhookHeaders(params.Headers)
	}
	if params.Retry != nil {
		policy, err := normalizeWebhookRetryPolicy(params.Retry)
		if err != nil {
			return webhookEndpoint{}, "", err
		}
		webhook.Retry = policy
	}

	rotatedSecret := ""
	if params.Secret != nil {
		webhook.Secret = strings.TrimSpace(*params.Secret)
	}
	if params.RotateSecret || strings.TrimSpace(webhook.Secret) == "" {
		nextSecret, err := randomHexToken(24)
		if err != nil {
			return webhookEndpoint{}, "", err
		}
		webhook.Secret = nextSecret
		rotatedSecret = nextSecret
	}

	webhook.UpdatedAt = time.Now().UTC()
	m.webhooks[webhookID] = webhook
	if err := m.persistStateLocked(webhook.UpdatedAt); err != nil {
		return webhookEndpoint{}, "", err
	}
	return webhook, rotatedSecret, nil
}

func (m *webhookManager) deleteWebhook(webhookID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.webhooks[webhookID]; !ok {
		return false, nil
	}
	delete(m.webhooks, webhookID)
	now := time.Now().UTC()
	for deliveryID, delivery := range m.deliveries {
		if delivery.WebhookID != webhookID {
			continue
		}
		if isWebhookDeliveryTerminal(delivery.Status) {
			continue
		}
		delivery.Status = webhookStatusCancelled
		delivery.NextAttemptAt = nil
		delivery.UpdatedAt = now
		m.deliveries[deliveryID] = delivery
	}
	if err := m.persistStateLocked(now); err != nil {
		return false, err
	}
	return true, nil
}

func paginateDeliveriesLocked(deliveries map[string]webhookDelivery, webhookID, status string, limit, offset int) ([]webhookDelivery, bool, int) {
	if limit <= 0 {
		limit = webhookDefaultListLimit
	}
	if limit > webhookMaxListLimit {
		limit = webhookMaxListLimit
	}
	if offset < 0 {
		offset = 0
	}

	filtered := make([]webhookDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.WebhookID != webhookID {
			continue
		}
		if status != "" && delivery.Status != status {
			continue
		}
		filtered = append(filtered, delivery)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
		}
		return filtered[i].ID > filtered[j].ID
	})
	if offset >= len(filtered) {
		return []webhookDelivery{}, false, offset
	}
	end := offset + limit
	hasMore := end < len(filtered)
	if end > len(filtered) {
		end = len(filtered)
	}
	nextOffset := end
	return append([]webhookDelivery(nil), filtered[offset:end]...), hasMore, nextOffset
}

func (m *webhookManager) listDeliveries(webhookID, status string, limit, offset int) ([]webhookDelivery, bool, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.webhooks[webhookID]; !ok {
		return nil, false, 0, os.ErrNotExist
	}
	items, hasMore, nextOffset := paginateDeliveriesLocked(m.deliveries, webhookID, status, limit, offset)
	return items, hasMore, nextOffset, nil
}

func (m *webhookManager) getDelivery(deliveryID string) (webhookDelivery, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	delivery, ok := m.deliveries[deliveryID]
	return delivery, ok
}

func (m *webhookManager) retryDelivery(deliveryID string) (webhookDelivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delivery, ok := m.deliveries[deliveryID]
	if !ok {
		return webhookDelivery{}, os.ErrNotExist
	}
	webhook, webhookExists := m.webhooks[delivery.WebhookID]
	if !webhookExists {
		return webhookDelivery{}, os.ErrNotExist
	}
	if !webhook.Enabled {
		return webhookDelivery{}, fmt.Errorf("webhook is disabled")
	}
	if delivery.Status == webhookStatusDelivering {
		return webhookDelivery{}, errors.New("delivery is currently in progress")
	}

	now := time.Now().UTC()
	delivery.Status = webhookStatusPending
	delivery.NextAttemptAt = &now
	delivery.UpdatedAt = now
	m.deliveries[deliveryID] = delivery
	if err := m.persistStateLocked(now); err != nil {
		return webhookDelivery{}, err
	}
	m.signalScheduler()
	return delivery, nil
}

func decodeListCursor(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0
	}
	return value
}
