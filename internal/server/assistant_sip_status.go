package server

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// A local host gateway publishes only connection settings, never credentials.
type assistantSIPStatus struct {
	Running   bool      `json:"running"`
	Server    string    `json:"server"`
	Port      int       `json:"port"`
	Phone     string    `json:"phone"`
	Account   string    `json:"account"`
	Extension string    `json:"extension"`
	Mode      string    `json:"mode"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (a *App) readAssistantSIPHostStatus() assistantSIPStatus {
	status := assistantSIPStatus{Port: 5060, Account: "1001", Extension: "1000", State: "stopped"}
	path := filepath.Join(a.dataPath, "sip", "host-status.json")
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Size() <= 4096 {
		if raw, err := os.ReadFile(path); err == nil {
			var saved assistantSIPStatus
			if json.Unmarshal(raw, &saved) == nil && net.ParseIP(saved.Server) != nil && net.ParseIP(saved.Phone) != nil && saved.Port >= 1024 && saved.Port <= 65535 && (saved.Mode == "normal" || saved.Mode == "read-only") {
				status = saved
				age := time.Since(saved.UpdatedAt)
				status.Running = saved.State != "stopped" && age >= -5*time.Second && age < 45*time.Second
			}
		}
	}
	return status
}

func (a *App) assistantSIPStatus(w http.ResponseWriter, r *http.Request) {
	status := a.readAssistantSIPHostStatus()
	a.mu.Lock()
	enabled := a.sipCallbacksEnabledLocked()
	jobs, err := a.sipCallbacksLocked()
	a.mu.Unlock()
	pending := 0
	last := ""
	number := 0
	if err == nil {
		for _, j := range jobs {
			if j.State == "queued" || j.State == "claimed" || j.State == "dialing" || j.State == "connected" {
				pending++
			}
			last = j.State
			number = j.Number
		}
	}
	raw, _ := json.Marshal(status)
	value := map[string]any{}
	_ = json.Unmarshal(raw, &value)
	value["callbacksEnabled"] = enabled
	value["callbackPending"] = pending
	value["callbackLastStatus"] = last
	value["callbackLastSession"] = number
	jsonOut(w, http.StatusOK, value)
}
