//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
	"golang.org/x/term"
)

func runOwnerView(args []string) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Use your root or authorized sudo session. / Root veya yetkili sudo oturumunu kullanın.")
		return exitNotOwner
	}
	o, ok := parseOwnerView(args)
	if !ok {
		fmt.Fprintln(os.Stderr, "Usage: recovery view --request-id <id> [--port 2084] [--lang en|tr]")
		return exitUsage
	}
	// A code is delivered only to the owner's controlling terminal, never stdout,
	// stderr, a URL, a file or the journal. No fallback for unattended invocation.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Open an interactive SSH terminal to start the temporary read-only view.")
		return exitUnavailable
	}
	defer tty.Close()
	if !term.IsTerminal(int(tty.Fd())) {
		return exitUnavailable
	}
	var code [32]byte
	defer clear(code[:])
	if _, err = rand.Read(code[:]); err != nil {
		return exitUnavailable
	}
	hash := sha256.Sum256(code[:])
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	err = serveOwnerView(ctx, o, hash, recoveryobs.Read, func() {
		fmt.Fprintln(tty, translated(o.lang,
			"Read-only recovery view. Forward the same loopback port through your SSH connection. Open the local address, then enter this temporary access code. It expires in 30 minutes; Ctrl+C closes access immediately.",
			"Salt-okur kurtarma ekranı. SSH bağlantınızla aynı yerel portu yönlendirin. Yerel adresi açıp bu geçici erişim kodunu girin. 30 dakika sonra dolar; Ctrl+C erişimi hemen kapatır."))
		fmt.Fprintf(tty, "http://127.0.0.1:%d/\n", o.port)
		fmt.Fprintln(tty, hex.EncodeToString(code[:]))
		clear(code[:])
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "The temporary recovery view could not run. Check that the local port is free; the recovery operation was not changed.")
		return exitUnavailable
	}
	return exitOK
}

func serveOwnerView(ctx context.Context, o ownerViewOptions, hash [32]byte, read func(string) recoveryobs.Status, ready func()) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(o.port))
	if err != nil {
		return err
	}
	defer listener.Close()
	deadline := time.Now().Add(ownerViewLifetime)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	server := &http.Server{Handler: ownerViewHandler(o, hash, deadline, time.Now, read), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 4096, ErrorLog: log.New(io.Discard, "", 0)}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-done:
		}
	}()
	defer close(done)
	ready()
	err = server.Serve(listener)
	if err == http.ErrServerClosed || ctx.Err() != nil {
		return nil
	}
	return err
}
