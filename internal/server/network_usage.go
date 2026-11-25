package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
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
	UploadBytes        int64                        `json:"uploadBytes"`
	DownloadBytes      int64                        `json:"downloadBytes"`
	LocalUploadBytes   int64                        `json:"localUploadBytes,omitempty"`
	LocalDownloadBytes int64                        `json:"localDownloadBytes,omitempty"`
	Interfaces         map[string]NetworkUsageBytes `json:"interfaces,omitempty"`
}

type networkUsageDelta struct {
	upload        int64
	download      int64
	localUpload   int64
	localDownload int64
	interfaces    map[string]NetworkUsageBytes
}

type networkUsageStore struct {
	mu                sync.Mutex
	path              string
	days              map[string]NetworkUsageDay
	legacyDays        map[string]NetworkUsageDay
	legacyExcluded    bool
	persistenceFailed bool
}

func newNetworkUsageStore(path string) *networkUsageStore {
	s := &networkUsageStore{path: path, days: map[string]NetworkUsageDay{}, legacyDays: map[string]NetworkUsageDay{}}
	b, err := os.ReadFile(path)
	if err == nil {
		var disk struct {
			Version    int                        `json:"version"`
			Days       map[string]NetworkUsageDay `json:"days"`
			LegacyDays map[string]NetworkUsageDay `json:"legacyDays"`
		}
		valid := json.Unmarshal(b, &disk) == nil && disk.Days != nil
		if valid {
			switch disk.Version {
			case 1:
				// Version 1 mixed workbench HTTP traffic with external communication.
				// Preserve the original ledger, but never present it as external usage.
				s.legacyDays = disk.Days
				s.legacyExcluded = len(disk.Days) > 0
			case 2:
				s.days = disk.Days
				if disk.LegacyDays != nil {
					s.legacyDays = disk.LegacyDays
					s.legacyExcluded = len(disk.LegacyDays) > 0
				}
			default:
				valid = false
			}
		}
		if !valid {
			// Never replace an unreadable historical ledger with an empty one.
			s.persistenceFailed = true
			log.Printf("network-traffic.json 无法解析或版本未知（保留原文件，统计仅保存在内存）")
		}
	} else if !os.IsNotExist(err) {
		s.persistenceFailed = true
		log.Printf("network-traffic.json 无法读取（统计仅保存在内存）: %v", err)
	}
	return s
}

func (s *networkUsageStore) add(delta networkUsageDelta, at time.Time) {
	if delta.upload == 0 && delta.download == 0 && delta.localUpload == 0 && delta.localDownload == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := at.UTC().Format("2006-01-02")
	day := s.days[key]
	day.UploadBytes += delta.upload
	day.DownloadBytes += delta.download
	day.LocalUploadBytes += delta.localUpload
	day.LocalDownloadBytes += delta.localDownload
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
	if err := atomicJSON(s.path, map[string]any{"version": 2, "days": s.days, "legacyDays": s.legacyDays}); err != nil {
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

func (s *networkUsageStore) snapshot() (map[string]NetworkUsageDay, NetworkUsageDay, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	days := map[string]NetworkUsageDay{}
	totals := NetworkUsageDay{Interfaces: map[string]NetworkUsageBytes{}}
	for key, day := range s.days {
		days[key] = cloneNetworkUsageDay(day)
		totals.UploadBytes += day.UploadBytes
		totals.DownloadBytes += day.DownloadBytes
		totals.LocalUploadBytes += day.LocalUploadBytes
		totals.LocalDownloadBytes += day.LocalDownloadBytes
		for name, value := range day.Interfaces {
			current := totals.Interfaces[name]
			current.UploadBytes += value.UploadBytes
			current.DownloadBytes += value.DownloadBytes
			totals.Interfaces[name] = current
		}
	}
	return days, totals, s.persistenceFailed, s.legacyExcluded
}

type networkByteCounter struct {
	received    atomic.Uint64
	transmitted atomic.Uint64
}

func (c *networkByteCounter) snapshot() networkCounter {
	if c == nil {
		return networkCounter{}
	}
	return networkCounter{received: c.received.Load(), transmitted: c.transmitted.Load()}
}

type networkCountingConn struct {
	net.Conn
	counter *networkByteCounter
}

func (c *networkCountingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.counter.received.Add(uint64(n))
	}
	return n, err
}

func (c *networkCountingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.counter.transmitted.Add(uint64(n))
	}
	return n, err
}

type networkCountingListener struct {
	net.Listener
	counter *networkByteCounter
}

func (l *networkCountingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &networkCountingConn{Conn: conn, counter: l.counter}, nil
}

type networkUsageMonitor struct {
	store            *networkUsageStore
	readCounters     func() (map[string]networkCounter, error)
	mu               sync.Mutex
	previous         map[string]networkCounter
	previousService  networkCounter
	serviceBytes     *networkByteCounter
	serviceInterface string
	initialized      bool
	available        bool
	source           string
	lastError        string
	sampledAt        time.Time
}

func newNetworkUsageMonitor(path string) *networkUsageMonitor {
	m := &networkUsageMonitor{
		store: newNetworkUsageStore(path), source: networkCounterSourceName(), readCounters: readNetworkCounters,
		serviceBytes: &networkByteCounter{}, serviceInterface: defaultNetworkInterface(),
	}
	m.sample(time.Now())
	return m
}

func (m *networkUsageMonitor) wrapServiceListener(listener net.Listener) net.Listener {
	if m == nil || m.serviceBytes == nil {
		return listener
	}
	return &networkCountingListener{Listener: listener, counter: m.serviceBytes}
}

func positiveNetworkDelta(current, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}

func externalNetworkBytes(raw, local uint64) (external int64, excluded int64) {
	if local > raw {
		local = raw
	}
	return int64(raw - local), int64(local)
}

func (m *networkUsageMonitor) sample(at time.Time) {
	// Serialize the read as well as the commit. Otherwise a slower, older read can
	// move the baseline backwards and count the same interface bytes again.
	m.mu.Lock()
	defer m.mu.Unlock()
	counters, err := m.readCounters()
	service := m.serviceBytes.snapshot()
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
		m.previousService = service
		m.initialized = true
		return
	}
	localUpload := positiveNetworkDelta(service.transmitted, m.previousService.transmitted)
	localDownload := positiveNetworkDelta(service.received, m.previousService.received)
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
		rawUpload := current.transmitted - previous.transmitted
		rawDownload := current.received - previous.received
		upload, excludedUpload := int64(rawUpload), int64(0)
		download, excludedDownload := int64(rawDownload), int64(0)
		if name == m.serviceInterface {
			upload, excludedUpload = externalNetworkBytes(rawUpload, localUpload)
			download, excludedDownload = externalNetworkBytes(rawDownload, localDownload)
			delta.localUpload += excludedUpload
			delta.localDownload += excludedDownload
		}
		value := NetworkUsageBytes{UploadBytes: upload, DownloadBytes: download}
		if value.UploadBytes == 0 && value.DownloadBytes == 0 {
			continue
		}
		delta.upload += value.UploadBytes
		delta.download += value.DownloadBytes
		delta.interfaces[name] = value
	}
	m.previous = counters
	m.previousService = service
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
	days, totals, persistenceWarning, legacyExcluded := a.networkUsage.store.snapshot()
	jsonOut(w, 200, map[string]any{
		"version": 2, "source": source, "supported": supported, "available": available,
		"readError": readError, "persistenceWarning": persistenceWarning, "legacyExcluded": legacyExcluded, "sampledAt": sampledAt,
		"measurement": "external-network-estimate", "scope": "aide-external-communication",
		"days": days, "totals": totals, "today": days[time.Now().UTC().Format("2006-01-02")],
	})
}
