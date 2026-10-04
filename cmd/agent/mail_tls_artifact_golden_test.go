package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailtlsartifact"
)

func TestMailTLSActualAlpha81PlanSharedProducerReader(t *testing.T) {
	for _, name := range []string{"empty", "sni"} {
		t.Run(name, func(t *testing.T) {
			raw, e := os.ReadFile("../../internal/mailtlsartifact/testdata/alpha81-" + name + ".json")
			if e != nil {
				t.Fatal(e)
			}
			historical, e := decodeMailTLSSyncJournal(raw)
			if e != nil {
				t.Fatal(e)
			}
			shared, e := mailtlsartifact.Decode(raw)
			if e != nil {
				t.Fatal(e)
			}
			got, e := encodeMailTLSSyncJournal(shared)
			if e != nil || !bytes.Equal(got, raw) {
				t.Fatalf("Agent writer differs from Alpha81: %v", e)
			}
			got, e = mailtlsartifact.Encode(historical)
			if e != nil || !bytes.Equal(got, raw) {
				t.Fatalf("shared writer differs from Alpha81: %v", e)
			}
			if !equalMailTLSSyncJournals(historical, shared) {
				t.Fatal("reader meaning differs")
			}
		})
	}
}
