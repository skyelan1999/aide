package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

var errNetworkUsageUnavailable = errors.New("网络流量统计不可用")

// NetworkUsageBytes is measured at the operating-system interface counter, not
// inferred from token usage or HTTP Content-Length headers.
type NetworkUsageBytes struct {
	UploadBytes   int64 `json:"uploadBytes"`
	DownloadBytes int64 `json:"downloadBytes"`
}

type NetworkUsageDay struct {
	UploadBytes   int64                        `json:"uploadBytes"`
	DownloadBytes int64                        `json:"downloadBytes"`
	Interfaces    map[string]NetworkUsageBytes `json:"interfaces,omitempty"`
}

type networkUsageDelta struct {
	upload     int64
	download   int64
	interfaces map[string]NetworkUsageBytes
}

type networkUsageStore struct {
	mu                sync.Mutex
	path              string
	days              map[string]NetworkUsageDay
	persistenceFailed bool
}

func newNetworkUsageStore(path string) *networkUsageStore {
	s := &networkUsageStore{path: path, days: map[string]NetworkUsageDay{}}
	b, err := os.ReadFile(path)
	if err == nil {
		var disk struct {
			Version int                        `json:"version"`
			Days    map[string]NetworkUsageDay `json:"days"`
		}
		if json.Unmarshal(b, &disk) != nil || disk.Version != 1 || disk.Days == nil {
			// Never replace an unreadable historical ledger with an empty one.
			s.persistenceFailed = true
			log.Printf("network-traffic.json 无法解析或版本未知（保留原文件，统计仅保存在内存）")
		} else {
			s.days = disk.Days
		}
	} else if !os.IsNotExist(err) {
		s.persistenceFailed = true
		log.Printf("network-traffic.json 无法读取（统计仅保存在内存）: %v", err)
	}
	return s
}

func (s *networkUsageStore) add(delta networkUsageDelta, at time.Time) {
	if delta.upload == 0 && delta.download == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := at.UTC().Format("2006-01-02")
	day := s.days[key]
	day.UploadBytes += delta.upload
	day.DownloadBytes += delta.download
	if day.Interfaces == nil {
		day.Interfaces = map[string]NetworkUsageBytes{}
	}
	for name, value := range delta.interfaces {
		current := day.Interfaces[name]
		current.UploadBytes += value.UploadBytes
		current.DownloadBytes += value.DownloadBytes
		day.Interfaces[name] = current
	}
	s.days[key] = day
	if s.persistenceFailed {
		return
	}
	if err := atomicJSON(s.path, map[string]any{"version": 1, "days": s.days}); err != nil {
		s.persistenceFailed = true
		log.Printf("network-traffic.json 保存失败（后续统计仅保存在内存）: %v", err)
	}
}

func cloneNetworkUsageDay(day NetworkUsageDay) NetworkUsageDay {
	copyDay := day
	copyDay.Interfaces = map[string]NetworkUsageBytes{}
	for name, value := range day.Interfaces {
		copyDay.Interfaces[name] = value
	}
	return copyDay
}

func (s *networkUsageStore) snapshot() (map[string]NetworkUsageDay, NetworkUsageDay, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	days := map[string]NetworkUsageDay{}
	totals := NetworkUsageDay{Interfaces: map[string]NetworkUsageBytes{}}
	for key, day := range s.days {
		days[key] = cloneNetworkUsageDay(day)
		totals.UploadBytes += day.UploadBytes
		totals.DownloadBytes += day.DownloadBytes
		for name, value := range day.Interfaces {
			current := totals.Interfaces[name]
			current.UploadBytes += value.UploadBytes
			current.DownloadBytes += value.DownloadBytes
			totals.Interfaces[name] = current
		}
	}
	return days, totals, s.persistenceFailed
}

type networkUsageMonitor struct {
	store        *networkUsageStore
	readCounters func() (map[string]networkCounter, error)
	mu           sync.Mutex
	previous     map[string]networkCounter
	initialized  bool
	available    bool
	source       string
	lastError    string
	sampledAt    time.Time
}

func newNetworkUsageMonitor(path string) *networkUsageMonitor {
	m := &networkUsageMonitor{store: newNetworkUsageStore(path), source: networkCounterSourceName(), readCounters: readNetworkCounters}
	m.sample(time.Now())
	return m
}

func (m *networkUsageMonitor) sample(at time.Time) {
	counters, err := m.readCounters()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sampledAt = at.UTC()
	if err != nil {
		m.lastError = "unavailable"
		m.available = false
		return
	}
	m.lastError = ""
	m.available = true
	if !m.initialized {
		m.previous = counters
		m.initialized = true
		return
	}
	delta := networkUsageDelta{interfaces: map[string]NetworkUsageBytes{}}
	for name, current := range counters {
		previous, exists := m.previous[name]
		if !exists {
			continue
		}
		// A process or container restart can reset interface counters. Rebase
		// instead of interpreting the lower value as a huge unsigned delta.
		if current.received < previous.received || current.transmitted < previous.transmitted {
			continue
		}
		value := NetworkUsageBytes{UploadBytes: int64(current.transmitted - previous.transmitted), DownloadBytes: int64(current.received - previous.received)}
		if value.UploadBytes == 0 && value.DownloadBytes == 0 {
			continue
		}
		delta.upload += value.UploadBytes
		delta.download += value.DownloadBytes
		delta.interfaces[name] = value
	}
	m.previous = counters
	m.store.add(delta, at)
}

func (m *networkUsageMonitor) run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case at := <-ticker.C:
			m.sample(at)
		case <-ctx.Done():
			m.sample(time.Now())
			return
		}
	}
}

func (m *networkUsageMonitor) status() (string, bool, bool, string, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.source, m.source != "unsupported", m.available, m.lastError, m.sampledAt
}

func (a *App) networkUsageHandler(w http.ResponseWriter, r *http.Request) {
	if a.networkUsage == nil {
		fail(w, 503, errNetworkUsageUnavailable)
		return
	}
	a.networkUsage.sample(time.Now())
	source, supported, available, readError, sampledAt := a.networkUsage.status()
	days, totals, persistenceWarning := a.networkUsage.store.snapshot()
	jsonOut(w, 200, map[string]any{
		"version": 1, "source": source, "supported": supported, "available": available,
		"readError": readError, "persistenceWarning": persistenceWarning, "sampledAt": sampledAt,
		"measurement": "network-interface-bytes", "scope": "aide-service-network-namespace",
		"days": days, "totals": totals, "today": days[time.Now().UTC().Format("2006-01-02")],
	})
}
