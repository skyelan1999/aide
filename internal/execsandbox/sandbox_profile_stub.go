//go:build !darwin

package execsandbox

func writeDarwinProfile(string, policy) (string, error) { return "", nil }
