//go:build linux

package main

import (
	"io"
	"strings"
	"testing"
)

func TestDatabaseHelperOutputRemainsBoundedThroughIOCopy(t *testing.T) {
	var out databaseResultBuffer
	if _, err := io.Copy(&out, strings.NewReader(strings.Repeat("x", 4097))); err == nil {
		t.Fatal("oversized helper output accepted")
	}
	if out.Len() > 4096 {
		t.Fatal("output escaped its bound")
	}
}
