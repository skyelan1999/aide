package main

import (
	"aide/internal/messaging"
	"testing"
)

func TestHelperCapabilitiesMatchSharedProtocols(t *testing.T) {
	caps := helperCapabilities()
	if caps["protocol"] != messaging.LANBridgeProtocolVersion {
		t.Fatalf("bridge protocol = %v", caps["protocol"])
	}
	features, ok := caps["capabilities"].(map[string]int)
	if !ok || features["message"] != messaging.LANBridgeProtocolVersion || features["capture"] != messaging.LANBridgeProtocolVersion || features["remoteAccess"] != messaging.LANRemoteAccessProtocolVersion {
		t.Fatalf("unexpected helper capabilities: %#v", caps)
	}
}
