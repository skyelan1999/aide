//go:build !windows

package hostbridge

func bridgeRootReplacementDenied(error) bool { return false }
