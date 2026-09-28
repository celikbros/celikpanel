package pdnspeerinspector

import (
	"bytes"
	"context"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	"strings"
	"testing"
	"time"
)

func TestServeOneCanonicalMinimalResponse(t *testing.T) {
	r, p, s := fixture(t)
	raw, err := pdnspeerproof.EncodeRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(append(raw, '\n')), &out, &testPolicy{p: p, hash: "policy"}, &testReader{first: s, second: s}, func() time.Time { return time.Unix(1800000003, 0) }); err != nil {
		t.Fatal(err)
	}
	response, err := pdnspeerproof.DecodeResponse(bytes.TrimSuffix(out.Bytes(), []byte("\n")))
	if err != nil || response.NativeState != "unloaded" {
		t.Fatalf("response: %+v %v", response, err)
	}
	if bytes.Contains(out.Bytes(), []byte("/var/lib")) {
		t.Fatal("native path leaked")
	}
}
func TestServeRejectsOversizedAndNoncanonical(t *testing.T) {
	_, p, s := fixture(t)
	for _, input := range [][]byte{bytes.Repeat([]byte("a"), 4098), []byte("{}"), []byte(strings.Repeat(" ", 20))} {
		var out bytes.Buffer
		if err := Serve(context.Background(), bytes.NewReader(input), &out, &testPolicy{p: p, hash: "policy"}, &testReader{first: s, second: s}, time.Now); err == nil || out.Len() != 0 {
			t.Fatalf("accepted bad request of %d bytes", len(input))
		}
	}
}
