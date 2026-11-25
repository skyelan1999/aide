package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNetworkUsageHandlerUsesInterfaceMeasurements(t *testing.T) {
	a := testApp(t)
	response := request(a, "GET", "/api/network-usage", nil)
	requireStatus(t, response, 200)
	body := response.Body.String()
	if !strings.Contains(body, `"measurement":"external-network-estimate"`) || !strings.Contains(body, `"days":`) {
		t.Fatalf("network response = %s", body)
	}
}

func TestNetworkUsageStorePersistsDailyAndInterfaceTotals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network-traffic.json")
	store := newNetworkUsageStore(path)
	day := time.Date(2026, 11, 24, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
	store.add(networkUsageDelta{upload: 100, download: 250, interfaces: map[string]NetworkUsageBytes{"eth0": {UploadBytes: 100, DownloadBytes: 250}}}, day)
	store.add(networkUsageDelta{upload: 25, download: 50, interfaces: map[string]NetworkUsageBytes{"eth0": {UploadBytes: 25, DownloadBytes: 50}}}, day.Add(time.Hour))

	reloaded := newNetworkUsageStore(path)
	days, totals, warning, _ := reloaded.snapshot()
	if warning {
		t.Fatal("unexpected persistence warning")
	}
	// 12:00 at UTC+8 is 04:00 UTC, so both samples belong to the same UTC day.
	got := days["2026-11-24"]
	if got.UploadBytes != 125 || got.DownloadBytes != 300 {
		t.Fatalf("daily totals = %+v", got)
	}
	if got.Interfaces["eth0"].UploadBytes != 125 || totals.Interfaces["eth0"].DownloadBytes != 300 {
		t.Fatalf("interface totals = %+v / %+v", got.Interfaces, totals.Interfaces)
	}
}

func TestNetworkUsageStoreDoesNotOverwriteCorruptLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network-traffic.json")
	original := []byte("{not-json")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	store := newNetworkUsageStore(path)
	store.add(networkUsageDelta{upload: 100, download: 200}, time.Now())
	_, _, warning, _ := store.snapshot()
	if !warning {
		t.Fatal("corrupt ledger must surface a persistence warning")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(original) {
		t.Fatalf("corrupt ledger was overwritten: %q, %v", got, err)
	}
}

func TestNetworkUsageMonitorAccumulatesAndRebasesResetCounters(t *testing.T) {
	samples := []map[string]networkCounter{
		{"eth0": {received: 1000, transmitted: 500}},
		{"eth0": {received: 1400, transmitted: 600}},
		{"eth0": {received: 10, transmitted: 5}}, // namespace restart
		{"eth0": {received: 110, transmitted: 45}},
	}
	monitor := &networkUsageMonitor{
		store: newNetworkUsageStore(filepath.Join(t.TempDir(), "network.json")), source: "linux-network-namespace",
		readCounters: func() (map[string]networkCounter, error) {
			current := samples[0]
			samples = samples[1:]
			return current, nil
		},
	}
	for i := 0; i < 4; i++ {
		monitor.sample(time.Date(2026, 11, 24, i, 0, 0, 0, time.UTC))
	}
	days, totals, warning, _ := monitor.store.snapshot()
	if warning || totals.UploadBytes != 140 || totals.DownloadBytes != 500 {
		t.Fatalf("totals = %+v, warning=%v", totals, warning)
	}
	got := days["2026-11-24"].Interfaces["eth0"]
	if got.UploadBytes != 140 || got.DownloadBytes != 500 {
		t.Fatalf("interface totals = %+v", got)
	}
}

func TestNetworkUsageMonitorExcludesWorkbenchHTTPBytes(t *testing.T) {
	samples := []map[string]networkCounter{
		{"eth0": {received: 1000, transmitted: 500}},
		{"eth0": {received: 1400, transmitted: 900}},
	}
	service := &networkByteCounter{}
	monitor := &networkUsageMonitor{
		store: newNetworkUsageStore(filepath.Join(t.TempDir(), "network.json")), source: "linux-network-namespace",
		serviceBytes: service, serviceInterface: "eth0",
		readCounters: func() (map[string]networkCounter, error) {
			current := samples[0]
			samples = samples[1:]
			return current, nil
		},
	}
	monitor.sample(time.Date(2026, 11, 25, 1, 0, 0, 0, time.UTC))
	service.received.Add(50)
	service.transmitted.Add(250)
	monitor.sample(time.Date(2026, 11, 25, 1, 0, 5, 0, time.UTC))

	days, totals, warning, _ := monitor.store.snapshot()
	if warning || totals.UploadBytes != 150 || totals.DownloadBytes != 350 || totals.LocalUploadBytes != 250 || totals.LocalDownloadBytes != 50 {
		t.Fatalf("totals = %+v, warning=%v", totals, warning)
	}
	got := days["2026-11-25"].Interfaces["eth0"]
	if got.UploadBytes != 150 || got.DownloadBytes != 350 {
		t.Fatalf("interface totals = %+v", got)
	}
}

func TestNetworkUsageStoreSeparatesLegacyMixedTraffic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network-traffic.json")
	legacy := []byte(`{"version":1,"days":{"2026-11-25":{"uploadBytes":180000000000,"downloadBytes":20}}}`)
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	store := newNetworkUsageStore(path)
	days, _, warning, legacyExcluded := store.snapshot()
	if warning || !legacyExcluded || len(days) != 0 {
		t.Fatalf("legacy migration days=%v warning=%v excluded=%v", days, warning, legacyExcluded)
	}
	store.add(networkUsageDelta{upload: 100, download: 200, localUpload: 300, localDownload: 400}, time.Date(2026, 11, 25, 2, 0, 0, 0, time.UTC))

	reloaded := newNetworkUsageStore(path)
	days, totals, warning, legacyExcluded := reloaded.snapshot()
	if warning || !legacyExcluded || totals.UploadBytes != 100 || totals.LocalUploadBytes != 300 {
		t.Fatalf("reloaded totals=%+v warning=%v excluded=%v", totals, warning, legacyExcluded)
	}
	if days["2026-11-25"].UploadBytes != 100 || reloaded.legacyDays["2026-11-25"].UploadBytes != 180000000000 {
		t.Fatalf("new and legacy ledgers were mixed: days=%+v legacy=%+v", days, reloaded.legacyDays)
	}
}
