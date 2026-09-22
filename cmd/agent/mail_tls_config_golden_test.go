package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailtlsartifact"
)

func TestMailTLSNativeConfigActualAlpha81Bytes(t *testing.T) {
	for _, kind := range []string{"empty", "sni"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "mailtlsartifact", "testdata", "alpha81-"+kind+".json"))
		if err != nil {
			t.Fatal(err)
		}
		plan, err := mailtlsartifact.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range []struct {
			name   string
			modern bool
		}{{"23", false}, {"24", true}} {
			want, err := os.ReadFile(filepath.Join("..", "..", "internal", "mailtlsconfig", "testdata", "alpha81-dovecot-"+d.name+"-"+kind+".conf"))
			if err != nil {
				t.Fatal(err)
			}
			if got := buildDovecotTLSConf(d.modern, defaultMailCert, defaultMailKey, plan.SNI); got != string(want) {
				t.Fatalf("current Agent changed Alpha81 Dovecot %s/%s bytes", d.name, kind)
			}
		}
		want, err := os.ReadFile(filepath.Join("..", "..", "internal", "mailtlsconfig", "testdata", "alpha81-postfix-"+kind+".map"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(expectedPostfixSNIMap(plan.SNI), want) {
			t.Fatal("current Agent changed Alpha81 SNI bytes")
		}
	}
}
