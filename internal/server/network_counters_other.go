//go:build !linux

package server

import "errors"

type networkCounter struct {
	received    uint64
	transmitted uint64
}

func networkCounterSourceName() string { return "unsupported" }

func defaultNetworkInterface() string { return "" }

func readNetworkCounters() (map[string]networkCounter, error) {
	return nil, errors.New("per-service network interface counters require Linux")
}
