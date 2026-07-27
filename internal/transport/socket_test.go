package transport

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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

type Slow struct{}

func (Slow) Wait(delay time.Duration, reply *bool) error {
	time.Sleep(delay)
	*reply = true
	return nil
}

type DispatchProbe struct {
	calls atomic.Int32
}

func (p *DispatchProbe) Mark(_ struct{}, reply *bool) error {
	p.calls.Add(1)
	*reply = true
	return nil
}

type LateReply struct {
	started sync.Once
	start   chan struct{}
	release chan struct{}
}

func (s *LateReply) Wait(_ struct{}, reply *string) error {
	s.started.Do(func() { close(s.start) })
	<-s.release
	*reply = "server-late-write"
	return nil
}

func testAgentConnector(
	socketPath string,
	token string,
) func(context.Context) (*rpc.Client, error) {
	return func(ctx context.Context) (*rpc.Client, error) {
		dialer := net.Dialer{Timeout: time.Second}
		conn, err := dialer.DialContext(ctx, "unix", socketPath)
		if err != nil {
			return nil, err
		}
		if err := clientHandshakeContext(ctx, conn, token); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return rpc.NewClient(conn), nil
	}
}

func pipeClient(t *testing.T, register func(*rpc.Server)) *rpc.Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := rpc.NewServer()
	register(server)
	go server.ServeConn(serverConn)
	client := rpc.NewClient(clientConn)
	t.Cleanup(func() { _ = client.Close() })
	return client
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
	if err := server.Register(Slow{}); err != nil {
		t.Fatalf("Register slow service: %v", err)
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

func TestReconnectingClientCallContextClosesTimedOutDedicatedCall(t *testing.T) {
	const token = "context-token"
	socketPath := startTestAgent(t, token)
	connector := testAgentConnector(socketPath, token)
	sharedClient, err := connector(context.Background())
	if err != nil {
		t.Fatalf("connect shared test client: %v", err)
	}
	defer sharedClient.Close()
	client := NewReconnectingClientWithContextConnector(sharedClient, connector)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	var reply bool
	err = client.CallContext(
		ctx, "Slow.Wait", 500*time.Millisecond, &reply,
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CallContext error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed >= 300*time.Millisecond {
		t.Fatalf("CallContext returned after %s, want cancellation before the RPC completes", elapsed)
	}

	var echoed string
	if err := client.Call("Echo.Ping", "shared-still-alive", &echoed); err != nil {
		t.Fatalf("shared client after dedicated timeout: %v", err)
	}
	if echoed != "shared-still-alive" {
		t.Fatalf("echo = %q", echoed)
	}
}

func TestConnectAgentContextCancelsAuthenticationHandshake(t *testing.T) {
	const token = "stalled-handshake-token"
	socketPath := filepath.Join(t.TempDir(), "stalled-agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	tokenPath := filepath.Join(t.TempDir(), "agent.token")
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_AGENT_SOCKET", socketPath)
	t.Setenv("CELIKPANEL_AGENT_TOKEN_FILE", tokenPath)

	accepted := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		// Read the presented token, but deliberately never acknowledge it.
		buf := make([]byte, len(token)+1)
		_, _ = conn.Read(buf)
		<-time.After(time.Second)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	client, err := ConnectAgentContext(ctx)
	if client != nil {
		_ = client.Close()
		t.Fatal("ConnectAgentContext returned a client after cancellation")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ConnectAgentContext error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(startedAt); elapsed >= 300*time.Millisecond {
		t.Fatalf("authentication cancellation took %s", elapsed)
	}
	select {
	case <-accepted:
	default:
		t.Fatal("test did not reach the authentication handshake")
	}
}

func TestCallContextRechecksCancellationBeforeDispatch(t *testing.T) {
	probe := &DispatchProbe{}
	ctx, cancel := context.WithCancel(context.Background())
	connector := func(context.Context) (*rpc.Client, error) {
		client := pipeClient(t, func(server *rpc.Server) {
			if err := server.RegisterName("Probe", probe); err != nil {
				t.Fatalf("register probe: %v", err)
			}
		})
		cancel()
		return client, nil
	}

	client := NewReconnectingClientWithContextConnector(nil, connector)
	var reply bool
	err := client.CallContext(ctx, "Probe.Mark", struct{}{}, &reply)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CallContext error = %v, want context canceled", err)
	}
	time.Sleep(20 * time.Millisecond)
	if calls := probe.calls.Load(); calls != 0 {
		t.Fatalf("server dispatch count = %d, want zero", calls)
	}
}

func TestCallContextCancellationDrainsReplyBeforeReturn(t *testing.T) {
	service := &LateReply{
		start:   make(chan struct{}),
		release: make(chan struct{}),
	}
	connector := func(context.Context) (*rpc.Client, error) {
		return pipeClient(t, func(server *rpc.Server) {
			if err := server.RegisterName("Late", service); err != nil {
				t.Fatalf("register late service: %v", err)
			}
		}), nil
	}
	client := NewReconnectingClientWithContextConnector(nil, connector)

	ctx, cancel := context.WithCancel(context.Background())
	reply := "initial"
	returned := make(chan error, 1)
	go func() {
		returned <- client.CallContext(ctx, "Late.Wait", struct{}{}, &reply)
	}()
	select {
	case <-service.start:
	case <-time.After(time.Second):
		t.Fatal("server method was not dispatched")
	}
	cancel()
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CallContext error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("CallContext did not return after cancellation")
	}

	reply = "caller-after-return"
	close(service.release)
	time.Sleep(30 * time.Millisecond)
	if reply != "caller-after-return" {
		t.Fatalf("reply changed after CallContext returned: %q", reply)
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
