//go:build linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceMutationSharedLedgerHistoricalWriter(t *testing.T) {
	paths, err := filepath.Glob("../../internal/servicemutationledger/testdata/alpha81-*.json")
	if err != nil || len(paths) != 11 {
		t.Fatalf("historical fixtures: %d %v", len(paths), err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := decodeServiceMutationLedger(raw)
			if err != nil {
				t.Fatal(err)
			}
			m, _ := newMutationTestManager(t)
			m.ledger = ledger
			if err = m.writeLocked(); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(m.ledgerPath)
			if err != nil || !bytes.Equal(actual, raw) {
				t.Fatalf("Agent producer changed historical bytes: %v", err)
			}
		})
	}
}
func TestServiceMutationSharedLedgerRefusesUnreadableWrite(t *testing.T) {
	m, _ := newMutationTestManager(t)
	raw, err := os.ReadFile("../../internal/servicemutationledger/testdata/alpha81-failed.json")
	if err != nil {
		t.Fatal(err)
	}
	l, err := decodeServiceMutationLedger(raw)
	if err != nil {
		t.Fatal(err)
	}
	m.ledger = l
	if err = m.writeLocked(); err != nil {
		t.Fatal(err)
	}
	for _, job := range m.ledger.Jobs {
		job.ErrorMessage = strings.Repeat("x", serviceMutationLedgerMaxSize)
	}
	if err = m.writeLocked(); err == nil {
		t.Fatal("published ledger exceeding reader size")
	}
	actual, err := os.ReadFile(m.ledgerPath)
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("last readable evidence lost: %v", err)
	}
	stages, err := filepath.Glob(filepath.Join(filepath.Dir(m.ledgerPath), ".service-mutations-*.json"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("invalid write staged evidence: %v %v", stages, err)
	}
}
