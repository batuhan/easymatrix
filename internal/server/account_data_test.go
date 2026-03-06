package server

import (
	"encoding/json"
	"testing"
)

func TestApplyRoomAccountDataContent_MarkedUnreadOverridesStaleArchive(t *testing.T) {
	state := roomAccountDataState{}

	archivedPayload, err := json.Marshal(beeperInboxDoneContent{
		UpdatedTS: ptrInt64(100),
	})
	if err != nil {
		t.Fatalf("marshal archive payload: %v", err)
	}
	state = applyRoomAccountDataContent(state, "com.beeper.inbox.done", archivedPayload)
	if !state.EffectiveArchived() {
		t.Fatalf("expected room to be archived after inbox.done")
	}

	markedUnreadPayload, err := json.Marshal(markedUnreadContent{
		Unread: true,
		TS:     200,
	})
	if err != nil {
		t.Fatalf("marshal marked unread payload: %v", err)
	}
	state = applyRoomAccountDataContent(state, "m.marked_unread", markedUnreadPayload)

	if !state.IsMarkedUnread {
		t.Fatalf("expected marked unread flag to be set")
	}
	if state.EffectiveArchived() {
		t.Fatalf("expected stale archive marker to be ignored after newer marked-unread event")
	}
}

func TestApplyRoomAccountDataContent_ParsesSnoozeState(t *testing.T) {
	state := roomAccountDataState{}
	payload, err := json.Marshal(snoozedContent{
		SnoozedUntilMS: ptrInt64(5000),
		UserSnoozedAt:  ptrInt64(4000),
	})
	if err != nil {
		t.Fatalf("marshal snooze payload: %v", err)
	}

	state = applyRoomAccountDataContent(state, "com.beeper.chats.snoozed", payload)

	if state.SnoozeUntilMS == nil || *state.SnoozeUntilMS != 5000 {
		t.Fatalf("expected snoozeUntilMs to be parsed, got %#v", state.SnoozeUntilMS)
	}
	if state.UserSnoozedAt == nil || *state.UserSnoozedAt != 4000 {
		t.Fatalf("expected userSnoozedAt to be parsed, got %#v", state.UserSnoozedAt)
	}
}

func ptrInt64(v int64) *int64 {
	return &v
}
