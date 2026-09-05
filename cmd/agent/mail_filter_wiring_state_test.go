package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// R-056. The whole point of this probe is that it can be asked before the
// caller takes the whole-host mutation lease, so what it says on a machine
// with no mail on it is the thing worth asserting.
func TestMailFilterWiringStateOnAHostWithNoMailServer(t *testing.T) {
	restore := mailServerBinaryLookup
	mailServerBinaryLookup = func(string) (string, error) {
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { mailServerBinaryLookup = restore })

	agent := &Agent{}
	var resp MailFilterWiringStateResponse
	if err := agent.MailFilterWiringState(&transport.Empty{}, &resp); err != nil {
		t.Fatalf("MailFilterWiringState returned an error: %v", err)
	}
	if resp.MailServerInstalled {
		t.Fatal("a host with no postconf reported a mail server")
	}
	if resp.Detail != mailFilterWiringAbsentDetail {
		t.Fatalf("detail = %q, want %q", resp.Detail, mailFilterWiringAbsentDetail)
	}
	// The detail is read by an operator looking at a brand new server, so it
	// has to name the host, not the code path.
	if !strings.Contains(resp.Detail, "no mail server is installed") {
		t.Fatalf("detail names nothing: %q", resp.Detail)
	}
}

func TestMailFilterWiringStateOnAHostWithPostfix(t *testing.T) {
	restore := mailServerBinaryLookup
	mailServerBinaryLookup = func(name string) (string, error) {
		if name != "postconf" {
			return "", errors.New("unexpected lookup: " + name)
		}
		return "/usr/sbin/postconf", nil
	}
	t.Cleanup(func() { mailServerBinaryLookup = restore })

	agent := &Agent{}
	var resp MailFilterWiringStateResponse
	if err := agent.MailFilterWiringState(&transport.Empty{}, &resp); err != nil {
		t.Fatalf("MailFilterWiringState returned an error: %v", err)
	}
	if !resp.MailServerInstalled {
		t.Fatal("a host with postconf reported no mail server")
	}
	if resp.Detail != mailFilterWiringPresentDetail {
		t.Fatalf("detail = %q, want %q", resp.Detail, mailFilterWiringPresentDetail)
	}
}

// The probe answers a question; it must never be the thing that changes a
// host. A response is overwritten rather than merged, so a caller reusing a
// struct cannot read a stale "installed" through it.
func TestMailFilterWiringStateOverwritesItsResponse(t *testing.T) {
	restore := mailServerBinaryLookup
	mailServerBinaryLookup = func(string) (string, error) {
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { mailServerBinaryLookup = restore })

	agent := &Agent{}
	resp := MailFilterWiringStateResponse{
		MailServerInstalled: true,
		Detail:              "stale",
	}
	if err := agent.MailFilterWiringState(&transport.Empty{}, &resp); err != nil {
		t.Fatalf("MailFilterWiringState returned an error: %v", err)
	}
	if resp.MailServerInstalled {
		t.Fatal("a stale installed flag survived the probe")
	}
}
