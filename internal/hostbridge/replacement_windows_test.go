package hostbridge

import (
	"errors"
	"golang.org/x/sys/windows"
)

func bridgeRootReplacementDenied(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
