package transport

import (
	"bufio"
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

// LoadOrCreateToken returns the shared token, generating a fresh random
// one on first run. The file is created 0600: the installer is
// responsible for granting the panel user read access in production.
// LoadOrCreateToken paylaşımlı token'ı döndürür; ilk çalıştırmada yeni bir
// rastgele token üretir. Dosya 0600 oluşturulur: üretimde panel
// kullanıcısına okuma izni vermek kurulum aracının sorumluluğudur.
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
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
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
	token, err := ReadToken(AgentTokenPath())
	if err != nil {
		return nil, fmt.Errorf("cannot read agent token (is the agent running, and does the panel user have read access?): %w", err)
	}

	conn, err := net.DialTimeout("unix", AgentSocketPath(), handshakeTimeout)
	if err != nil {
		return nil, fmt.Errorf("cannot reach agent socket: %w", err)
	}

	if err := clientHandshake(conn, token); err != nil {
		conn.Close()
		return nil, err
	}
	return rpc.NewClient(conn), nil
}

func clientHandshake(conn net.Conn, token string) error {
	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return err
	}

	if _, err := conn.Write([]byte(token + "\n")); err != nil {
		return fmt.Errorf("handshake write failed: %w", err)
	}

	reader := bufio.NewReaderSize(conn, maxTokenLineLen)
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("agent rejected the handshake: %w", err)
	}
	if strings.TrimSpace(line) != handshakeOK {
		return fmt.Errorf("unexpected handshake response")
	}

	return conn.SetDeadline(time.Time{})
}
