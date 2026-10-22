//go:build !linux

package server

import (
	"context"
	"errors"
)

func executePWImage(context.Context, []byte, []byte) ([]byte, error) {
	return nil, errors.New("PW sealed runtime execution requires Linux")
}
