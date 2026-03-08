package server

import (
	"log"

	"go.mau.fi/gomuks/pkg/gomuks"
	"go.mau.fi/gomuks/pkg/hicli/jsoncmd"
)

func (m *webhookManager) subscribeRuntimeEvents() error {
	if m == nil || m.server == nil || m.server.rt == nil {
		return nil
	}
	buffer := m.server.rt.EventBuffer()
	if buffer == nil {
		return nil
	}
	listenerID, _ := buffer.Subscribe(0, nil, func(evt *gomuks.BufferedEvent) {
		if evt == nil {
			return
		}
		syncComplete, ok := evt.Data.(*jsoncmd.SyncComplete)
		if !ok || syncComplete == nil {
			return
		}
		select {
		case m.eventCh <- syncComplete:
		default:
			// Drop overflowing events instead of blocking sync processing.
		}
	})
	m.unsubscribe = func() {
		if runtimeBuffer := m.server.rt.EventBuffer(); runtimeBuffer != nil {
			runtimeBuffer.Unsubscribe(listenerID)
		}
	}
	return nil
}

func (m *webhookManager) runEventLoop() {
	for {
		select {
		case <-m.stopCh:
			return
		case syncComplete := <-m.eventCh:
			if syncComplete == nil {
				continue
			}
			m.handleSyncComplete(syncComplete)
		}
	}
}

func (m *webhookManager) handleSyncComplete(syncComplete *jsoncmd.SyncComplete) {
	if m == nil || m.server == nil {
		return
	}
	domainEvents := mapSyncCompleteToDomainEvents(syncComplete)
	for _, domainEvent := range domainEvents {
		if !isSupportedWebhookEventType(domainEvent.Type) || domainEvent.Type == webhookEventTypeTest {
			continue
		}

		payloadObject := map[string]any{
			"chatID": domainEvent.ChatID,
			"ids":    append([]string(nil), domainEvent.IDs...),
		}
		if domainEvent.Type == webhookEventTypeMessageUpsert {
			entries, err := m.server.hydrateMessagesForWSEvent(domainEvent.ChatID, domainEvent.IDs)
			if err != nil {
				log.Printf("failed to hydrate message payload for webhook event: %v", err)
			} else if len(entries) > 0 {
				payloadObject["entries"] = entries
			}
		}

		envelope, err := newWebhookEnvelope(domainEvent.Type, payloadObject, "sync")
		if err != nil {
			log.Printf("failed to create webhook event envelope: %v", err)
			continue
		}
		if _, err = m.enqueueEventForMatchingWebhooks(domainEvent.Type, envelope); err != nil {
			log.Printf("failed to enqueue webhook deliveries: %v", err)
		}
	}
}
