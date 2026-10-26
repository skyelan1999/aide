package server

import (
	"encoding/json"
	"testing"
)

func TestIssue88StandaloneWorkbenchCapabilitiesGetBridge(t *testing.T) {
	for _, kind := range []string{"project", "mobile", "both", "none"} {
		t.Run(kind, func(t *testing.T) {
			a := workbenchFixture(t)
			for i := range a.pluginRegistry.Plugins {
				p := &a.pluginRegistry.Plugins[i]
				if p.ID == "workbench-fixture" {
					p.Workbench.ProjectMessages = kind == "project" || kind == "both"
					p.Workbench.MobileCapture = kind == "mobile" || kind == "both"
				}
			}
			out := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", true)
			requireStatus(t, out, 200)
			var opened struct {
				URL    string `json:"url"`
				Bridge *struct {
					Instance string   `json:"instanceId"`
					Tools    []string `json:"tools"`
					Project  bool     `json:"projectMessages"`
					Mobile   bool     `json:"mobileCapture"`
				} `json:"bridge"`
			}
			if err := json.Unmarshal(out.Body.Bytes(), &opened); err != nil {
				t.Fatal(err)
			}
			if kind == "none" {
				if opened.Bridge != nil {
					t.Fatal("ordinary page unexpectedly got bridge")
				}
				return
			}
			if opened.Bridge == nil || opened.Bridge.Instance == "" {
				t.Fatal("declared capability received no bridge")
			}
			if len(opened.Bridge.Tools) != 0 || opened.Bridge.Project != (kind != "mobile") || opened.Bridge.Mobile != (kind != "project") {
				t.Fatal("bridge advertised wrong capabilities")
			}
			requireStatus(t, workbenchRequest(a, "GET", opened.URL, false), 200)
			requestID := "standalone"
			args := map[string]any{"projectName": "AI", "fromDate": "2026-10-24", "toDate": "2026-10-24"}
			call := request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+opened.Bridge.Instance+"/call", map[string]any{"protocol": 1, "requestId": requestID, "method": "project.messages", "arguments": args})
			want := 200
			if !opened.Bridge.Project {
				want = 403
			}
			requireStatus(t, call, want)
			if opened.Bridge.Mobile {
				if _, _, err := ensureTLSCert(CertsDir(a.dataPath)); err != nil {
					t.Fatal(err)
				}
			}
			capture := request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+opened.Bridge.Instance+"/call", map[string]any{"protocol": 1, "requestId": "capture_create", "method": "capture.control", "arguments": map[string]any{"action": "create", "title": "Owned capture", "mode": "receipt", "allowUnknown": true, "baseline": "owned-baseline", "baseURL": "https://localhost", "zones": []string{"A"}}})
			captureWant := 403
			if opened.Bridge.Mobile {
				captureWant = 200
			}
			requireStatus(t, capture, captureWant)
			denied := request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+opened.Bridge.Instance+"/call", map[string]any{"protocol": 1, "requestId": "undeclared_tool", "method": "tool.call", "tool": "not_declared", "arguments": map[string]any{}})
			requireStatus(t, denied, 403)
		})
	}
}
