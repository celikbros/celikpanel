//go:build !linux

package main

import (
	"fmt"
	"os"
	"runtime"
)

func secureOpenLogFile(_, _ string, _ bool) (*os.File, error) {
	return nil, fmt.Errorf("secure log file access is unsupported on %s", runtime.GOOS)
}
