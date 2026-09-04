//go:build !linux

package main

import "fmt"

func countUsableUsersInSerializedSQLiteDatabase(_ []byte) (int, error) {
	return 0, fmt.Errorf("read-only WAL-aware user admission is supported only on Linux")
}
