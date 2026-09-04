//go:build !linux

package main

import (
	"errors"
	"os"
)

func readAdminCredentialsFile(_ *os.File) (adminCredentials, error) {
	return adminCredentials{}, errors.New("admin credentials file input is supported only on Linux")
}
