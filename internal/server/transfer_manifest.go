package server

import (
	"encoding/json"
	"errors"
)

const (
	maxTransferEntries       = 100000
	maxTransferManifestBytes = 16 << 20
)

// Count scanned entries too, including excluded names, so an invalid inventory
// cannot avoid the work bound. Encoded bytes include JSON escaping of paths.
type transferManifestBudget struct {
	entries int
	bytes   int
}

func (b *transferManifestBudget) discover() error {
	if b.entries >= maxTransferEntries {
		return errors.New("传输目录条目超过100000项，请拆分任务")
	}
	b.entries++
	return nil
}

func (b *transferManifestBudget) add(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(encoded)+1 > maxTransferManifestBytes-b.bytes {
		return errors.New("传输清单超过16MiB，请拆分任务")
	}
	b.bytes += len(encoded) + 1
	return nil
}

func validateTransferManifestSize(j *transferJob) error {
	// Match atomicJSON's on-disk representation, including indentation.
	encoded, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	if len(encoded) > maxTransferManifestBytes {
		return errors.New("传输清单超过16MiB，请拆分任务")
	}
	return nil
}
