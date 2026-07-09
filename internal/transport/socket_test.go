package transport

import (
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Echo is a minimal RPC service used to prove that an authenticated
// connection actually serves RPC end to end.
// Echo, kimliği doğrulanmış bir bağlantının uçtan uca gerçekten RPC
// sunduğunu kanıtlamak için kullanılan en küçük RPC servisidir.
type Echo struct{}

func (Echo) Ping(msg string, reply *string) error {
	*reply = msg
	return nil
}

func startTestAgent(t *testing.T, token string) string {
	t.Helper()

	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := ListenAgent(socketPath)
	if err != nil {
		t.Fatalf("ListenAgent: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	server := rpc.NewServer()
	if err := server.Register(Echo{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go ServeAgentConn(server, conn, token)
		}
	}()

	return socketPath
}

func TestHandshakeAndRPC(t *testing.T) {
	const token = "correct-token"
	socketPath := startTestAgent(t, token)

	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := clientHandshake(conn, token); err != nil {
		t.Fatalf("clientHandshake: %v", err)
	}

	client := rpc.NewClient(conn)
	defer client.Close()

	var reply string
	if err := client.Call("Echo.Ping", "merhaba", &reply); err != nil {
		t.Fatalf("Echo.Ping: %v", err)
	}
	if reply != "merhaba" {
		t.Fatalf("got %q, want %q", reply, "merhaba")
	}
}

func TestHandshakeRejectsWrongToken(t *testing.T) {
	socketPath := startTestAgent(t, "correct-token")

	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := clientHandshake(conn, "wrong-token"); err == nil {
		t.Fatal("handshake with wrong token succeeded, want rejection")
	}
}

func TestHandshakeRejectsSilentClient(t *testing.T) {
	// A client that never sends a token must be dropped by the deadline,
	// not hold a goroutine forever.
	// Hiç token göndermeyen bir istemci, süresiz goroutine tutmak yerine
	// süre sınırıyla düşürülmelidir.
	socketPath := startTestAgent(t, "correct-token")

	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 8)
	conn.SetReadDeadline(time.Now().Add(2 * handshakeTimeout))
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("server responded to a silent client, want closed connection")
	}
}

func TestLoadOrCreateToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "agent.token")

	created, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatalf("LoadOrCreateToken (create): %v", err)
	}
	if len(created) != 64 {
		t.Fatalf("token length = %d, want 64 hex chars", len(created))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	// 0640: the token is group-readable (root:celikpanel) so the low-privilege
	// panel, which runs in the celikpanel group, can read it to reach the agent
	// socket. LoadOrCreateToken writes 0640 to match; the directory is 0750.
	// 0640: token grup-okunur (root:celikpanel); böylece celikpanel grubunda
	// koşan düşük-yetkili panel, agent socket'ine ulaşmak için onu okuyabilir.
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Fatalf("token file permissions = %o, want 640", perm)
	}

	loaded, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatalf("LoadOrCreateToken (load): %v", err)
	}
	if loaded != created {
		t.Fatal("second call regenerated the token, want stable load")
	}
}

func TestListenAgentReplacesStaleSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "agent.sock")

	first, err := ListenAgent(socketPath)
	if err != nil {
		t.Fatalf("first ListenAgent: %v", err)
	}
	// Simulate a crash: the socket file stays behind without a listener.
	// Çökme simülasyonu: socket dosyası dinleyicisi olmadan geride kalır.
	first.Close()

	second, err := ListenAgent(socketPath)
	if err != nil {
		t.Fatalf("ListenAgent over stale socket: %v", err)
	}
	second.Close()
}
