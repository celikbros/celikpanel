//go:build linux

package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
	paneldb "github.com/alicelik/celikpanel/internal/db"
)

// Run the production main in a separate process with a real SQLite database,
// real TLS listener and no Agent socket. No host services or installed state are
// changed; every writable path and the listener belong to this test.
func TestPanelRecoveryProcessHelper(t *testing.T) {
	if os.Getenv("CELIKPANEL_RECOVERY_PROCESS_TEST") != "1" {
		return
	}
	flag.CommandLine = flag.NewFlagSet("panel-recovery-process", flag.ExitOnError)
	os.Args = []string{os.Args[0]}
	main()
}

func TestPanelProcessPreservesHTTPSAndAuthenticationWhileAgentAbsent(t *testing.T) {
	root := t.TempDir()
	web := filepath.Join(root, "web")
	if err := os.Mkdir(web, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("process recovery shell"), 0600); err != nil {
		t.Fatal(err)
	}
	database, err := paneldb.NewSQLiteDB(filepath.Join(root, "celikpanel.db"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("local recovery process test password")
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.GetDB().Exec(`INSERT INTO users (username,password_hash,email,role,status) VALUES ('recovery-admin',?,'admin@example.test','admin','active')`, hash)
	database.Close()
	if err != nil {
		t.Fatal(err)
	}
	cert, key := filepath.Join(root, "test.crt"), filepath.Join(root, "test.key")
	if err = generateSelfSigned(cert, key); err != nil {
		t.Fatal(err)
	}
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	_ = reservation.Close()
	logPath := filepath.Join(root, "process.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestPanelRecoveryProcessHelper$")
	// Strip inherited CelikPanel settings so this child cannot contact installed
	// infrastructure, even when the parent test runner has development settings.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CELIKPANEL_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env,
		"CELIKPANEL_RECOVERY_PROCESS_TEST=1", "CELIKPANEL_DATA_DIR="+root,
		"CELIKPANEL_WEB_DIR="+web, "CELIKPANEL_LISTEN="+address,
		"CELIKPANEL_TLS_CERT="+cert, "CELIKPANEL_TLS_KEY="+key,
		"CELIKPANEL_AGENT_SOCKET="+filepath.Join(root, "absent-agent.sock"),
		"CELIKPANEL_AGENT_TOKEN_FILE="+filepath.Join(root, "absent-agent.token"))
	cmd.Dir = root
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	t.Cleanup(func() {
		if !exited {
			_ = cmd.Process.Kill()
			<-done
		}
		if t.Failed() {
			raw, _ := os.ReadFile(logPath)
			t.Logf("panel child output:\n%s", raw)
		}
	})
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // Loopback fixture certificate only.
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	base := "https://" + address
	deadline := time.Now().Add(15 * time.Second)
	for {
		response, getErr := client.Get(base + "/setup")
		if getErr == nil {
			raw, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode == 200 && strings.Contains(string(raw), "process recovery shell") {
				break
			}
		}
		select {
		case processErr := <-done:
			exited = true
			t.Fatalf("panel exited before recovery became available: %v", processErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("recovery listener did not start: %v", getErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
	login, err := http.NewRequest("POST", base+"/api/v1/auth/login", bytes.NewBufferString(`{"username":"recovery-admin","password":"local recovery process test password"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("Origin", base)
	response, err := client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("login without Agent: %d %s", response.StatusCode, raw)
	}
	var cookie *http.Cookie
	for _, c := range response.Cookies() {
		if c.Name == sessionCookieName {
			cookie = c
		}
	}
	if cookie == nil || !cookie.Secure {
		t.Fatal("authenticated secure session was not established")
	}
	request := func(method, path string) (int, map[string]any) {
		t.Helper()
		req, requestErr := http.NewRequest(method, base+path, nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		req.AddCookie(cookie)
		req.Header.Set("Origin", base)
		res, requestErr := client.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer res.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, body
	}
	for attempt := 0; attempt < 3; attempt++ {
		code, body := request("GET", panelAvailabilityPath)
		if code != 200 || body["state"] != "starting" {
			t.Fatalf("availability: %d %v", code, body)
		}
		code, body = request("GET", "/api/v1/auth/me")
		if code != 200 || body["username"] != "recovery-admin" {
			t.Fatalf("session: %d %v", code, body)
		}
		code, body = request("GET", panelRecoveryStatusPath+"?request_id="+recoveryHTTPTestID)
		if code != 200 || body["request_id"] != recoveryHTTPTestID || body["observation"] != "unavailable" || body["terminal_proof"] != "none" {
			t.Fatalf("unknown recovery: %d %v", code, body)
		}
		if _, exists := body["phase"]; exists {
			t.Fatalf("unknown observation invented a phase: %v", body)
		}
		code, body = request("POST", "/api/v1/system/update/start")
		if code != 503 {
			t.Fatalf("management mutation admitted: %d %v", code, body)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		exited = true
		if err != nil {
			t.Fatalf("panel did not shut down cleanly: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("panel did not stop promptly while Agent remained absent")
	}
	logs, _ := os.ReadFile(logPath)
	if strings.Count(string(logs), "Starting CelikPanel Backend") != 1 || strings.Contains(string(logs), "Connected to Agent RPC") {
		t.Fatalf("unexpected process restart or connection: %s", logs)
	}
	t.Log("Production main stayed HTTPS-reachable; real admin login and same-session reads worked; management was blocked; owner SIGTERM exited cleanly without Agent.")
}
