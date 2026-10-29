package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestUpdateAgentFailureResultDoesNotClaimRecovery(t *testing.T) {
	a := testApp(t)
	a.buildRuntimeMode = "release-image"
	op := strings.Repeat("c", 32)
	state := updateSlotsState{
		Version: 1, ActiveSlot: "A", Slots: map[string]updateSlot{},
		Pending: &updateOperation{ID: op, Target: "B", Status: "rollback", Progress: 95},
	}
	a.updatesMu.Lock()
	err := a.saveUpdateStateLocked(state)
	a.updatesMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	message := "Activation failed: original failure; Recovery: failed: recovery launcher failed"
	result := map[string]any{"operationId": op, "success": false, "message": message}
	w := request(a, http.MethodPost, "/api/updates/agent/result", result)
	requireStatus(t, w, http.StatusOK)
	var got updateSlotsState
	if err = json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Pending != nil || got.ActiveSlot != "A" || got.LastMessage != "槽切换失败："+message {
		t.Fatalf("failed recovery must finish pending operation without claiming rollback: %+v", got)
	}
	// Client outbox retries must not be allowed to finish a newer operation.
	state.Pending = &updateOperation{ID: strings.Repeat("d", 32), Target: "B", Status: "requested"}
	a.updatesMu.Lock()
	err = a.saveUpdateStateLocked(state)
	a.updatesMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, http.MethodPost, "/api/updates/agent/result", result), http.StatusConflict)
	a.updatesMu.Lock()
	after, err := a.loadUpdateStateLocked()
	a.updatesMu.Unlock()
	if err != nil || after.Pending == nil || after.Pending.ID != state.Pending.ID {
		t.Fatalf("stale result changed new pending operation: state=%+v err=%v", after, err)
	}
}
