package transport

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The Agent RPC runs as root. It is reachable only through a local Unix
// socket, and every connection must present the shared token before any
// RPC call is served. Both layers are required: socket permissions guard
// the transport, the token guards against misconfigured permissions.
//
// Agent RPC'si root olarak çalışır. Yalnızca yerel bir Unix socket
// üzerinden erişilebilir ve her bağlantı, herhangi bir RPC çağrısından
// önce paylaşımlı token'ı sunmak zorundadır. İki katman da gereklidir:
// socket izinleri taşımayı, token ise yanlış yapılandırılmış izinleri
// korur.

const (
	// handshakeTimeout bounds how long an unauthenticated connection may
	// hold the accept loop's goroutine.
	// handshakeTimeout, kimliği doğrulanmamış bir bağlantının accept
	// döngüsünün goroutine'ini ne kadar tutabileceğini sınırlar.
	handshakeTimeout = 5 * time.Second

	// maxTokenLineLen guards the handshake reader against unbounded input.
	// maxTokenLineLen, el sıkışma okuyucusunu sınırsız girdiye karşı korur.
	maxTokenLineLen = 512

	handshakeOK = "OK"
)

// AgentSocketPath resolves the Unix socket path. Production (root) uses
// /run, development falls back to the local data directory.
// AgentSocketPath, Unix socket yolunu çözer. Üretim (root) /run kullanır,
// geliştirme yerel data dizinine düşer.
func AgentSocketPath() string {
	if p := os.Getenv("CELIKPANEL_AGENT_SOCKET"); p != "" {
		return p
	}
	if os.Geteuid() == 0 {
		return "/run/celikpanel/agent.sock"
	}
	return "./data/agent.sock"
}

// AgentTokenPath resolves the shared token file path.
// AgentTokenPath, paylaşımlı token dosyasının yolunu çözer.
func AgentTokenPath() string {
	if p := os.Getenv("CELIKPANEL_AGENT_TOKEN_FILE"); p != "" {
		return p
	}
	if os.Geteuid() == 0 {
		return "/etc/celikpanel/agent.token"
	}
	return "./data/agent.token"
}

// LoadOrCreateToken returns the shared token, generating a fresh random one
// on first run. The file is 0640 inside a 0750 directory: only the owner
// (root agent) may write it, and only the owning group may read it. In
// production the installer runs the agent with Group=celikpanel and puts
// the panel user in that group, so the low-privilege panel — and nothing
// else — can read the token. In dev, agent and panel share one user, so
// owner read suffices either way.
//
// LoadOrCreateToken paylaşımlı token'ı döndürür; ilk çalıştırmada yeni bir
// rastgele token üretir. Dosya, 0750 bir dizin içinde 0640'tır: yalnızca
// sahibi (root agent) yazar, yalnızca sahip grup okur. Üretimde kurulum
// agent'ı Group=celikpanel ile çalıştırır ve panel kullanıcısını o gruba
// koyar; böylece yalnızca düşük yetkili panel token'ı okuyabilir.
func LoadOrCreateToken(path string) (string, error) {
	if token, err := ReadToken(path); err == nil {
		return token, nil
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	token := hex.EncodeToString(raw)

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("failed to create token directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o640); err != nil {
		return "", fmt.Errorf("failed to write token file: %w", err)
	}
	return token, nil
}

// ReadToken reads and trims the shared token from disk.
// ReadToken, paylaşımlı token'ı diskten okur ve kırpar.
func ReadToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return token, nil
}

// ListenAgent opens the Unix socket, replacing any stale socket file left
// by a previous run. Permissions are tightened before the listener is
// returned so there is no window with a world-accessible socket.
// ListenAgent, Unix socket'i açar ve önceki çalıştırmadan kalan eski
// socket dosyasını değiştirir. İzinler, dinleyici döndürülmeden önce
// sıkılaştırılır; böylece herkese açık bir socket penceresi oluşmaz.
func ListenAgent(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove stale socket: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		listener.Close()
		return nil, fmt.Errorf("failed to set socket permissions: %w", err)
	}
	return listener, nil
}

// ServeAgentConn authenticates one inbound connection and, on success,
// serves RPC on it until the peer disconnects.
// ServeAgentConn, gelen bir bağlantının kimliğini doğrular ve başarılı
// olursa karşı taraf bağlantıyı kesene kadar üzerinde RPC sunar.
func ServeAgentConn(server *rpc.Server, conn net.Conn, token string) {
	defer func() {
		// rpc.ServeConn closes the connection itself; closing again is a
		// harmless no-op and covers the handshake-failure path.
		// rpc.ServeConn bağlantıyı kendisi kapatır; tekrar kapatmak
		// zararsızdır ve el sıkışma hatası yolunu da kapsar.
		conn.Close()
	}()

	if err := serverHandshake(conn, token); err != nil {
		return
	}
	server.ServeConn(conn)
}

func serverHandshake(conn net.Conn, token string) error {
	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return err
	}

	reader := bufio.NewReaderSize(conn, maxTokenLineLen)
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("handshake read failed: %w", err)
	}

	presented := strings.TrimSpace(line)
	if !hmac.Equal([]byte(presented), []byte(token)) {
		return fmt.Errorf("handshake token mismatch")
	}

	if _, err := conn.Write([]byte(handshakeOK + "\n")); err != nil {
		return err
	}
	// Clear the handshake deadline; RPC connections are long-lived.
	// El sıkışma süre sınırını kaldır; RPC bağlantıları uzun ömürlüdür.
	return conn.SetDeadline(time.Time{})
}

// ConnectAgent dials the agent socket, authenticates with the shared
// token and returns a ready RPC client. This is the only way the panel
// talks to the agent.
// ConnectAgent, agent socket'ine bağlanır, paylaşımlı token ile kimlik
// doğrular ve hazır bir RPC istemcisi döndürür. Panelin agent ile
// konuşmasının tek yolu budur.
func ConnectAgent() (*rpc.Client, error) {
	return ConnectAgentContext(context.Background())
}

// ConnectAgentContext dials and authenticates an agent connection while
// honoring cancellation during both socket dialing and the authentication
// handshake.
func ConnectAgentContext(ctx context.Context) (*rpc.Client, error) {
	if ctx == nil {
		return nil, fmt.Errorf("connect agent context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	token, err := ReadToken(AgentTokenPath())
	if err != nil {
		return nil, fmt.Errorf("cannot read agent token (is the agent running, and does the panel user have read access?): %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dialer := net.Dialer{Timeout: handshakeTimeout}
	conn, err := dialer.DialContext(ctx, "unix", AgentSocketPath())
	if err != nil {
		if ctxErr := contextCompletionError(ctx); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("cannot reach agent socket: %w", err)
	}

	if err := clientHandshakeContext(ctx, conn, token); err != nil {
		conn.Close()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		conn.Close()
		return nil, err
	}
	return rpc.NewClient(conn), nil
}

func clientHandshake(conn net.Conn, token string) error {
	return clientHandshakeContext(context.Background(), conn, token)
}

func clientHandshakeContext(ctx context.Context, conn net.Conn, token string) error {
	if ctx == nil {
		return fmt.Errorf("handshake context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	deadline := time.Now().Add(handshakeTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	cancelClosed := make(chan struct{})
	stopCancelClose := context.AfterFunc(ctx, func() {
		_ = conn.Close()
		close(cancelClosed)
	})

	if _, err := conn.Write([]byte(token + "\n")); err != nil {
		if !stopCancelClose() {
			<-cancelClosed
		}
		if ctxErr := contextCompletionError(ctx); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("handshake write failed: %w", err)
	}

	reader := bufio.NewReaderSize(conn, maxTokenLineLen)
	line, err := reader.ReadString('\n')
	if err != nil {
		if !stopCancelClose() {
			<-cancelClosed
		}
		if ctxErr := contextCompletionError(ctx); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("agent rejected the handshake: %w", err)
	}
	if strings.TrimSpace(line) != handshakeOK {
		if !stopCancelClose() {
			<-cancelClosed
		}
		if ctxErr := contextCompletionError(ctx); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("unexpected handshake response")
	}

	if !stopCancelClose() {
		<-cancelClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return conn.SetDeadline(time.Time{})
}

// contextCompletionError also recognizes the narrow scheduling window where
// an I/O deadline fires at the context's deadline just before the context
// timer goroutine publishes Done/Err.
func contextCompletionError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
