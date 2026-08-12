//go:build !linux

package main

import (
	"crypto/ed25519"
	"errors"
)

func loadSystemUpdatePublicKey(string) (ed25519.PublicKey, error) {
	return nil, errors.New("system updates are supported only on Linux")
}
