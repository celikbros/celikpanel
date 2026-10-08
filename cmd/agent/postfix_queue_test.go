package main

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

func installFakePostqueue(t *testing.T, installed bool, list func() ([]byte, error)) {
	t.Helper()
	oldLook, oldList := postfixQueueLookPath, postfixQueueList
	t.Cleanup(func() { postfixQueueLookPath, postfixQueueList = oldLook, oldList })
	postfixQueueLookPath = func(string) (string, error) {
		if !installed {
			return "", exec.ErrNotFound
		}
		return "/usr/sbin/postqueue", nil
	}
	postfixQueueList = list
}

const twoQueuedMessages = `{"queue_name": "deferred", "queue_id": "4F2C1A0B", "arrival_time": 1791500000, "message_size": 2048, "forced_expire": false, "sender": "shop@example.com", "recipients": [{"address": "a@example.net", "delay_reason": "connect to mx.example.net: Connection timed out"}]}
{"queue_name": "active", "queue_id": "9D3E7C11", "arrival_time": 1791500100, "message_size": 512, "forced_expire": false, "sender": "info@example.com", "recipients": [{"address": "b@example.net"}]}
`

// The defect: every failure of `postqueue -j` was answered as "not installed,
// no items", and the screen said the queue was empty over a queue nobody had
// read. `postqueue -j` fails whenever the mail system is down, which is exactly
// when a queue is likely to hold something.
func TestPostfixQueueTellsAFailedReadFromAnEmptyQueue(t *testing.T) {
	unreadable := map[string]func() ([]byte, error){
		"the mail system is down": func() ([]byte, error) {
			return nil, errors.New("exit status 69")
		},
		"a line that is not a queue entry": func() ([]byte, error) {
			return []byte(twoQueuedMessages + "postqueue: warning: something else on standard output\n"), nil
		},
		"an entry without a queue ID": func() ([]byte, error) {
			return []byte(`{"queue_name": "deferred", "sender": "x@example.com"}` + "\n"), nil
		},
	}
	for name, list := range unreadable {
		t.Run(name, func(t *testing.T) {
			installFakePostqueue(t, true, list)
			var result core.PostfixQueueResult
			err := (&Agent{}).PostfixQueue(&transport.Empty{}, &result)
			if err == nil || err.Error() != transport.PostfixQueueUnreadable {
				t.Fatalf("err = %v, want %q", err, transport.PostfixQueueUnreadable)
			}
			if len(result.Items) != 0 || result.Summary != (core.PostfixSummary{}) {
				t.Fatalf("a failed read still carried part of a queue: %+v", result)
			}
		})
	}

	t.Run("a queue that is empty", func(t *testing.T) {
		installFakePostqueue(t, true, func() ([]byte, error) { return nil, nil })
		var result core.PostfixQueueResult
		if err := (&Agent{}).PostfixQueue(&transport.Empty{}, &result); err != nil {
			t.Fatal(err)
		}
		if !result.Installed || result.Items == nil || len(result.Items) != 0 {
			t.Fatalf("an empty queue = %+v, want installed with an empty list", result)
		}
	})

	t.Run("a queue with messages", func(t *testing.T) {
		installFakePostqueue(t, true, func() ([]byte, error) { return []byte(twoQueuedMessages), nil })
		var result core.PostfixQueueResult
		if err := (&Agent{}).PostfixQueue(&transport.Empty{}, &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Items) != 2 || result.Items[0].ID != "4F2C1A0B" || result.Items[0].Status != "deferred" ||
			result.Summary.Deferred != 1 || result.Summary.Active != 1 {
			t.Fatalf("queue = %+v", result)
		}
	})

	t.Run("Postfix is not on this server", func(t *testing.T) {
		installFakePostqueue(t, false, func() ([]byte, error) {
			t.Fatal("postqueue was run although it is not installed")
			return nil, nil
		})
		var result core.PostfixQueueResult
		if err := (&Agent{}).PostfixQueue(&transport.Empty{}, &result); err != nil {
			t.Fatal(err)
		}
		if result.Installed || len(result.Items) != 0 {
			t.Fatalf("result = %+v, want the known 'not installed' answer", result)
		}
	})
}
