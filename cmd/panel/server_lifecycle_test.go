package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServePanelHTTPUntilGracefullyStopsListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	server := &http.Server{
		Addr: listener.Addr().String(),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			closeOnce(started)
			_, _ = io.WriteString(w, "ok")
		}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- servePanelHTTPUntil(
			ctx,
			server,
			func() error { return server.Serve(listener) },
		)
	}()

	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	<-started
	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("graceful serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("graceful serve did not return")
	}
}

func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
