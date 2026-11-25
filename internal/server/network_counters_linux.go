//go:build linux

package server

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

type networkCounter struct {
	received    uint64
	transmitted uint64
}

func networkCounterSourceName() string { return "linux-network-namespace" }

func defaultNetworkInterface() string {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[1] == "00000000" {
			return fields[0]
		}
	}
	return ""
}

// /proc/self/net/dev is scoped to the caller's network namespace. In Aide's
// service container this covers model HTTP(S), media, plugin downloads,
// realtime sockets, SSH and protocol overhead from every process in that
// namespace. Loopback is deliberately excluded.
func readNetworkCounters() (map[string]networkCounter, error) {
	file, err := os.Open("/proc/self/net/dev")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := map[string]networkCounter{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		name, fields, found := strings.Cut(line, ":")
		if !found || name == "lo" {
			continue
		}
		parts := strings.Fields(fields)
		if len(parts) < 16 {
			continue
		}
		received, receiveErr := strconv.ParseUint(parts[0], 10, 64)
		transmitted, transmitErr := strconv.ParseUint(parts[8], 10, 64)
		if receiveErr == nil && transmitErr == nil {
			result[name] = networkCounter{received: received, transmitted: transmitted}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, errors.New("no non-loopback network interface")
	}
	return result, nil
}
