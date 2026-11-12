// Copyright © 2017 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: BSD-2-Clause
package xdr

import (
	"io"

	xdr "github.com/rasky/go-xdr/xdr2"
)

func Read(r io.Reader, val interface{}) error {
	_, err := xdr.UnmarshalLimited(r, val, 1<<20)
	return err
}

func ReadUint32(r io.Reader) (uint32, error) {
	var n uint32
	if err := Read(r, &n); err != nil {
		return n, err
	}

	return n, nil
}

func ReadOpaque(r io.Reader) ([]byte, error) {
	length, err := ReadUint32(r)
	if err != nil {
		return nil, err
	}

	if length > 1<<20 {
		return nil, io.ErrShortBuffer
	}
	buf := make([]byte, length)
	if _, err = io.ReadFull(r, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func ReadUint32List(r io.Reader) ([]uint32, error) {
	length, err := ReadUint32(r)
	if err != nil {
		return nil, err
	}

	if length > 1024 {
		return nil, io.ErrShortBuffer
	}
	buf := make([]uint32, length)

	for i := 0; i < int(length); i++ {
		buf[i], err = ReadUint32(r)
		if err != nil {
			return nil, err
		}
	}

	return buf, nil
}
