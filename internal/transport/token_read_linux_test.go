//go:build linux

package transport

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadTokenKeepsRegularSymlinkCompatibility(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "token")
	alias := filepath.Join(root, "alias")
	if err := os.WriteFile(path, []byte("fixture-token\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadToken(alias); err != nil || got != "fixture-token" {
		t.Fatalf("existing regular token alias refused: %q %v", got, err)
	}
}

func TestReadTokenFIFODoesNotBlockAgentConnection(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "token.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "fifo-alias")
	if err := os.Symlink(fifo, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fifo, alias} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Setenv("CELIKPANEL_AGENT_TOKEN_FILE", path)
			t.Setenv("CELIKPANEL_AGENT_SOCKET", filepath.Join(root, "unused.sock"))
			done := make(chan error, 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				client, err := ConnectAgentContext(ctx)
				if client != nil {
					client.Close()
				}
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("non-regular token accepted")
				}
			case <-time.After(time.Second):
				cancel()
				// Release a regressed blocking FIFO reader before failing this test.
				// Bu test başarısız olmadan engellenen FIFO okuyucusunu serbest bırak.
				if fd, err := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
					unix.Write(fd, []byte("fixture-token\n"))
					unix.Close(fd)
				}
				t.Fatal("agent connection blocked while reading a non-regular token")
			}
		})
	}
}

func TestLoadOrCreateTokenPreservesExistingFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := LoadOrCreateToken(path)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("existing FIFO accepted or token regenerated")
		}
	case <-time.After(time.Second):
		// Unblock a regressed reader or writer without replacing the FIFO.
		// Gerileyen okuyucu/yazıcıyı FIFO'yu değiştirmeden serbest bırak.
		if fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0); err == nil {
			defer unix.Close(fd)
			unix.Write(fd, []byte("fixture-token\n"))
			select {
			case <-done:
			case <-time.After(time.Second):
			}
		}
		t.Fatal("token producer blocked on existing FIFO")
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) || after.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("token producer changed existing FIFO")
	}
}
