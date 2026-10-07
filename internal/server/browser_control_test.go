package server

import (
	"strings"
	"testing"
)

func TestBrowserToolConfirmationBoundary(t *testing.T) {
	for _, name := range []string{"browser_navigate", "browser_click", "browser_fill"} {
		if !browserToolRequiresConfirmation(name) {
			t.Errorf("%s must require confirmation", name)
		}
	}
	for _, name := range []string{"browser_status", "browser_snapshot", "get_current_datetime"} {
		if browserToolRequiresConfirmation(name) {
			t.Errorf("%s must remain read-only", name)
		}
	}
}

func TestBrowserActionConfirmationContainsTarget(t *testing.T) {
	got := browserActionConfirmationText("browser_navigate", map[string]any{"url": "https://www.dji.com/", "approved": false})
	if got == "" || !strings.Contains(got, "导航到网页") || !strings.Contains(got, "https://www.dji.com/") {
		t.Fatalf("confirmation omits operation or target: %q", got)
	}
}

func TestBrowserFillConfirmationDoesNotStoreTypedContent(t *testing.T) {
	got := browserActionConfirmationText("browser_fill", map[string]any{"selector": "input[name=q]", "text": "private sample", "approved": false})
	if !strings.Contains(got, "input[name=q]") || !strings.Contains(got, "characterCount") || strings.Contains(got, "private sample") {
		t.Fatalf("confirmation should summarize typed content without persisting it: %q", got)
	}
}

func TestComputerToolConfirmationBoundary(t *testing.T) {
	for _, name := range []string{"computer_click", "computer_type", "computer_key"} {
		if !computerToolRequiresConfirmation(name) {
			t.Errorf("%s must require confirmation", name)
		}
	}
	for _, name := range []string{"computer_status", "computer_snapshot", "browser_snapshot"} {
		if computerToolRequiresConfirmation(name) {
			t.Errorf("%s must remain read-only", name)
		}
	}
}

func TestComputerConfirmationDoesNotStoreTypedContent(t *testing.T) {
	got := pluginActionConfirmationText("computer-control", "computer_type", map[string]any{"text": "private sample", "approved": false})
	if !strings.Contains(got, "键入文字") || !strings.Contains(got, "characterCount") || strings.Contains(got, "private sample") {
		t.Fatalf("confirmation should summarize typed content without persisting it: %q", got)
	}
}
