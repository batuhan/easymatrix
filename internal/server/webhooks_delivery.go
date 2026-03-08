package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
)

func (m *webhookManager) runScheduler() {
	defer close(m.stoppedCh)

	ticker := time.NewTicker(webhookSchedulerTick)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.scheduleDueDeliveries()
		case <-m.wakeCh:
			m.scheduleDueDeliveries()
		}
	}
}

func (m *webhookManager) scheduleDueDeliveries() {
	now := time.Now().UTC()

	m.mu.Lock()
	dueDeliveryIDs := make([]string, 0, len(m.deliveries))
	for deliveryID, delivery := range m.deliveries {
		switch delivery.Status {
		case webhookStatusPending, webhookStatusRetryScheduled:
		default:
			continue
		}
		if delivery.NextAttemptAt != nil && delivery.NextAttemptAt.After(now) {
			continue
		}
		delivery.Status = webhookStatusDelivering
		delivery.UpdatedAt = now
		delivery.NextAttemptAt = nil
		m.deliveries[deliveryID] = delivery
		dueDeliveryIDs = append(dueDeliveryIDs, deliveryID)
	}
	if len(dueDeliveryIDs) > 0 {
		if err := m.persistStateLocked(now); err != nil {
			log.Printf("failed to persist webhook state before scheduling deliveries: %v", err)
		}
	}
	m.mu.Unlock()

	for _, deliveryID := range dueDeliveryIDs {
		select {
		case m.deliverySem <- struct{}{}:
			go func(id string) {
				defer func() { <-m.deliverySem }()
				m.executeDelivery(id)
			}(deliveryID)
		default:
			m.mu.Lock()
			delivery, ok := m.deliveries[deliveryID]
			if ok && delivery.Status == webhookStatusDelivering {
				nextAt := now.Add(500 * time.Millisecond)
				delivery.Status = webhookStatusRetryScheduled
				delivery.NextAttemptAt = &nextAt
				delivery.UpdatedAt = now
				m.deliveries[deliveryID] = delivery
				_ = m.persistStateLocked(now)
			}
			m.mu.Unlock()
		}
	}
}

func (m *webhookManager) executeDelivery(deliveryID string) {
	m.mu.RLock()
	delivery, deliveryExists := m.deliveries[deliveryID]
	webhook, webhookExists := m.webhooks[delivery.WebhookID]
	m.mu.RUnlock()
	if !deliveryExists {
		return
	}
	if !webhookExists || !webhook.Enabled {
		now := time.Now().UTC()
		m.mu.Lock()
		current, ok := m.deliveries[deliveryID]
		if ok {
			current.Status = webhookStatusCancelled
			current.NextAttemptAt = nil
			current.LastError = "webhook is disabled or deleted"
			current.UpdatedAt = now
			m.deliveries[deliveryID] = current
			_ = m.persistStateLocked(now)
		}
		m.mu.Unlock()
		return
	}

	startedAt := time.Now().UTC()
	attemptNum := delivery.AttemptCount + 1
	timestamp := strconv.FormatInt(startedAt.Unix(), 10)
	standardSignature, legacySignature := buildWebhookSignatures(delivery.EventID, timestamp, delivery.Payload, webhook.Secret)

	timeout := time.Duration(webhook.Retry.TimeoutMS) * time.Millisecond
	requestCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, webhook.URL, bytes.NewReader(delivery.Payload))
	if err != nil {
		m.finalizeDeliveryAttempt(deliveryID, webhook, attemptNum, startedAt, 0, err.Error(), false)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-EasyMatrix-Webhook-ID", webhook.ID)
	req.Header.Set("X-EasyMatrix-Delivery-ID", delivery.ID)
	req.Header.Set("X-EasyMatrix-Event-ID", delivery.EventID)
	req.Header.Set("X-EasyMatrix-Event-Type", delivery.EventType)
	req.Header.Set("X-EasyMatrix-Attempt", strconv.Itoa(attemptNum))
	req.Header.Set("X-EasyMatrix-Timestamp", timestamp)
	req.Header.Set("X-EasyMatrix-Signature", legacySignature)
	req.Header.Set("webhook-id", delivery.EventID)
	req.Header.Set("webhook-timestamp", timestamp)
	req.Header.Set("webhook-signature", standardSignature)
	for key, value := range webhook.Headers {
		req.Header.Set(key, value)
	}

	httpClient := &http.Client{Timeout: timeout}
	resp, err := httpClient.Do(req)
	if resp != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
	}
	if err != nil {
		m.finalizeDeliveryAttempt(deliveryID, webhook, attemptNum, startedAt, 0, err.Error(), true)
		return
	}

	statusCode := resp.StatusCode
	if statusCode >= 200 && statusCode < 300 {
		m.finalizeDeliveryAttempt(deliveryID, webhook, attemptNum, startedAt, statusCode, "", false)
		return
	}

	retryable := statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests || statusCode >= 500
	m.finalizeDeliveryAttempt(deliveryID, webhook, attemptNum, startedAt, statusCode, "webhook returned HTTP "+strconv.Itoa(statusCode), retryable)
}

func (m *webhookManager) finalizeDeliveryAttempt(
	deliveryID string,
	webhook webhookEndpoint,
	attemptNum int,
	startedAt time.Time,
	responseStatus int,
	errMessage string,
	retryable bool,
) {
	finishedAt := time.Now().UTC()
	duration := finishedAt.Sub(startedAt)

	m.mu.Lock()
	defer m.mu.Unlock()

	delivery, ok := m.deliveries[deliveryID]
	if !ok {
		return
	}
	currentWebhook, webhookExists := m.webhooks[delivery.WebhookID]
	if !webhookExists {
		delivery.Status = webhookStatusCancelled
		delivery.UpdatedAt = finishedAt
		delivery.NextAttemptAt = nil
		delivery.LastError = "webhook deleted"
		m.deliveries[deliveryID] = delivery
		_ = m.persistStateLocked(finishedAt)
		return
	}

	attempt := webhookDeliveryAttempt{
		Attempt:        attemptNum,
		StartedAt:      startedAt,
		DurationMS:     duration.Milliseconds(),
		ResponseStatus: responseStatus,
		Error:          errMessage,
		Retryable:      retryable,
	}
	delivery.Attempts = append(delivery.Attempts, attempt)
	if len(delivery.Attempts) > webhookMaxPersistedAttempts {
		delivery.Attempts = append([]webhookDeliveryAttempt(nil), delivery.Attempts[len(delivery.Attempts)-webhookMaxPersistedAttempts:]...)
	}
	delivery.AttemptCount = attemptNum
	delivery.LastAttemptAt = &finishedAt
	delivery.ResponseStatus = responseStatus
	delivery.LastError = errMessage
	delivery.UpdatedAt = finishedAt
	delivery.NextAttemptAt = nil

	if errMessage == "" {
		delivery.Status = webhookStatusSucceeded
		delivery.LastError = ""
		currentWebhook.ConsecutiveFailures = 0
		currentWebhook.LastSuccessAt = &finishedAt
	} else {
		currentWebhook.ConsecutiveFailures++
		currentWebhook.LastFailureAt = &finishedAt

		shouldRetry := retryable && currentWebhook.Enabled && attemptNum < currentWebhook.Retry.MaxAttempts
		if currentWebhook.Retry.AutoDisableAfterFailures > 0 && currentWebhook.ConsecutiveFailures >= currentWebhook.Retry.AutoDisableAfterFailures {
			currentWebhook.Enabled = false
			currentWebhook.DisabledReason = "auto-disabled after consecutive delivery failures"
			shouldRetry = false
		}
		if shouldRetry {
			delay := nextWebhookRetryDelay(currentWebhook.Retry, attemptNum)
			nextAttemptAt := finishedAt.Add(delay)
			delivery.Status = webhookStatusRetryScheduled
			delivery.NextAttemptAt = &nextAttemptAt
		} else {
			delivery.Status = webhookStatusFailed
		}
	}

	currentWebhook.UpdatedAt = finishedAt
	m.webhooks[currentWebhook.ID] = currentWebhook
	m.deliveries[delivery.ID] = delivery
	if err := m.persistStateLocked(finishedAt); err != nil {
		log.Printf("failed to persist webhook delivery result: %v", err)
	}
	if delivery.Status == webhookStatusRetryScheduled {
		m.signalScheduler()
	}
}

func nextWebhookRetryDelay(policy webhookRetryPolicy, failureCount int) time.Duration {
	expo := backoff.NewExponentialBackOff()
	expo.InitialInterval = time.Duration(policy.InitialBackoffMS) * time.Millisecond
	expo.Multiplier = policy.BackoffMultiplier
	expo.RandomizationFactor = policy.JitterFactor
	expo.MaxInterval = time.Duration(policy.MaxBackoffMS) * time.Millisecond
	expo.MaxElapsedTime = 0
	expo.Reset()

	delay := expo.InitialInterval
	if failureCount < 1 {
		return delay
	}
	for i := 0; i < failureCount; i++ {
		next := expo.NextBackOff()
		if next == backoff.Stop {
			break
		}
		delay = next
	}
	if delay < expo.InitialInterval {
		delay = expo.InitialInterval
	}
	return delay
}

func (m *webhookManager) enqueueEventForMatchingWebhooks(eventType string, envelope webhookEventEnvelope) ([]webhookDelivery, error) {
	if !isSupportedWebhookEventType(eventType) {
		return nil, nil
	}
	rawPayload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()

	created := make([]webhookDelivery, 0)
	for _, webhook := range m.webhooks {
		if !webhook.matchesEvent(eventType) {
			continue
		}
		deliveryID, tokenErr := randomHexToken(16)
		if tokenErr != nil {
			return nil, tokenErr
		}
		nextAttempt := now
		delivery := webhookDelivery{
			ID:            deliveryID,
			WebhookID:     webhook.ID,
			EventID:       envelope.ID,
			EventType:     eventType,
			Payload:       append([]byte(nil), rawPayload...),
			Status:        webhookStatusPending,
			AttemptCount:  0,
			NextAttemptAt: &nextAttempt,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		m.deliveries[delivery.ID] = delivery
		created = append(created, delivery)
	}
	if len(created) == 0 {
		return []webhookDelivery{}, nil
	}
	if err = m.persistStateLocked(now); err != nil {
		return nil, err
	}
	m.signalScheduler()
	return created, nil
}

func (m *webhookManager) enqueueEventForWebhook(webhookID, eventType string, envelope webhookEventEnvelope) ([]webhookDelivery, error) {
	rawPayload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	webhook, ok := m.webhooks[webhookID]
	if !ok {
		return nil, os.ErrNotExist
	}
	if !webhook.Enabled {
		return nil, errors.New("webhook is disabled")
	}

	now := time.Now().UTC()
	deliveryID, err := randomHexToken(16)
	if err != nil {
		return nil, err
	}
	nextAttempt := now
	delivery := webhookDelivery{
		ID:            deliveryID,
		WebhookID:     webhook.ID,
		EventID:       envelope.ID,
		EventType:     eventType,
		Payload:       append([]byte(nil), rawPayload...),
		Status:        webhookStatusPending,
		AttemptCount:  0,
		NextAttemptAt: &nextAttempt,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	m.deliveries[delivery.ID] = delivery
	if err = m.persistStateLocked(now); err != nil {
		return nil, err
	}
	m.signalScheduler()
	return []webhookDelivery{delivery}, nil
}

func newWebhookEnvelope(eventType string, object any, source string) (webhookEventEnvelope, error) {
	eventID, err := randomHexToken(16)
	if err != nil {
		return webhookEventEnvelope{}, err
	}
	envelope := webhookEventEnvelope{
		ID:       eventID,
		Type:     eventType,
		Created:  time.Now().UTC().Unix(),
		Livemode: true,
		Data: webhookEventData{
			Object: object,
		},
	}
	source = strings.TrimSpace(source)
	if source != "" {
		envelope.Request = &webhookEventRequest{
			ID:     eventID,
			Source: source,
		}
	}
	return envelope, nil
}
