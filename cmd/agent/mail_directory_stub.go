//go:build !linux

package main

import (
	"fmt"
	"os"
)

func ensureMailboxDirectory(_, _ string) (func() error, error) {
	if os.Getenv("CELIKPANEL_MAIL_DIR") != "" {
		return nil, nil
	}
	return nil, fmt.Errorf("secure mailbox directory creation requires Linux openat2")
}
