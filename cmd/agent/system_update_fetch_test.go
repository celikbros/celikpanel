package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func signedSystemUpdateFixture(t *testing.T) (systemUpdateManifest, []byte, []byte, ed25519.PublicKey) {
	t.Helper()
	manifest := testSystemUpdateManifest()
	raw, err := canonicalSystemUpdateManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, raw, ed25519.Sign(privateKey, raw), publicKey
}

func testHTTPSystemUpdateFetcher(server *httptest.Server, publicKey ed25519.PublicKey) *httpSystemUpdateManifestFetcher {
	client := server.Client()
	client.Timeout = time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &httpSystemUpdateManifestFetcher{origin: server.URL, client: client, loadKey: func() (ed25519.PublicKey, error) { return append(ed25519.PublicKey(nil), publicKey...), nil }}
}

func TestHTTPSystemUpdateFetcherVerifiesExactSignedPlatformManifest(t *testing.T) {
	manifest, raw, signature, publicKey := signedSystemUpdateFixture(t)
	wantBase := "/releases/" + manifest.Version + "/linux/amd64/release-manifest-v2"
	seen := map[string]int{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen[request.URL.Path]++
		switch request.URL.Path {
		case "/releases/latest.txt":
			writer.Write([]byte(manifest.Version + "\n"))
		case wantBase:
			writer.Write(raw)
		case wantBase + ".sig":
			writer.Write(signature)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	fetcher := testHTTPSystemUpdateFetcher(server, publicKey)
	version, err := fetcher.Discover(context.Background())
	if err != nil || version != manifest.Version {
		t.Fatalf("discover = %q, %v", version, err)
	}
	got, err := fetcher.Fetch(context.Background(), version, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got != manifest {
		t.Fatalf("manifest = %#v, want %#v", got, manifest)
	}
	if seen[wantBase] != 1 || seen[wantBase+".sig"] != 1 {
		t.Fatalf("unexpected paths: %#v", seen)
	}
}

func TestHTTPSystemUpdateFetcherRejectsTamperWrongKeyRedirectAndLimits(t *testing.T) {
	manifest, raw, signature, publicKey := signedSystemUpdateFixture(t)
	base := "/releases/" + manifest.Version + "/linux/amd64/release-manifest-v2"
	t.Run("tampered", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, ".sig") {
				writer.Write(signature)
				return
			}
			writer.Write(append(append([]byte(nil), raw...), 'x'))
		}))
		defer server.Close()
		if _, err := testHTTPSystemUpdateFetcher(server, publicKey).Fetch(context.Background(), manifest.Version, "linux", "amd64"); err == nil {
			t.Fatal("tampered manifest accepted")
		}
	})
	t.Run("wrong-key", func(t *testing.T) {
		wrong, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, ".sig") {
				writer.Write(signature)
			} else {
				writer.Write(raw)
			}
		}))
		defer server.Close()
		if _, err := testHTTPSystemUpdateFetcher(server, wrong).Fetch(context.Background(), manifest.Version, "linux", "amd64"); err == nil {
			t.Fatal("wrong key accepted")
		}
	})
	t.Run("off-origin-redirect", func(t *testing.T) {
		evil := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.Write(raw) }))
		defer evil.Close()
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == base {
				http.Redirect(writer, request, evil.URL+"/manifest", http.StatusFound)
				return
			}
			writer.Write(signature)
		}))
		defer server.Close()
		if _, err := testHTTPSystemUpdateFetcher(server, publicKey).Fetch(context.Background(), manifest.Version, "linux", "amd64"); err == nil {
			t.Fatal("off-origin redirect accepted")
		}
	})
	t.Run("manifest-limit", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, ".sig") {
				writer.Write(signature)
				return
			}
			writer.Write([]byte(strings.Repeat("x", systemUpdateMaxManifestSize+1)))
		}))
		defer server.Close()
		if _, err := testHTTPSystemUpdateFetcher(server, publicKey).Fetch(context.Background(), manifest.Version, "linux", "amd64"); err == nil {
			t.Fatal("oversized manifest accepted")
		}
	})
	t.Run("signature-limit", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, ".sig") {
				writer.Write(append(signature, 0))
				return
			}
			writer.Write(raw)
		}))
		defer server.Close()
		if _, err := testHTTPSystemUpdateFetcher(server, publicKey).Fetch(context.Background(), manifest.Version, "linux", "amd64"); err == nil {
			t.Fatal("oversized signature accepted")
		}
	})
}

func TestHTTPSystemUpdateFetcherTimeoutAndDiscoveryLimits(t *testing.T) {
	_, _, _, publicKey := signedSystemUpdateFixture(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("slow") == "1" {
			<-request.Context().Done()
			return
		}
		writer.Write([]byte(strings.Repeat("v", systemUpdateLatestMaxSize+1)))
	}))
	defer server.Close()
	fetcher := testHTTPSystemUpdateFetcher(server, publicKey)
	if _, err := fetcher.Discover(context.Background()); err == nil {
		t.Fatal("oversized discovery accepted")
	}
	fetcher.origin = server.URL + "?slow=1"
	if _, err := fetcher.Discover(context.Background()); err == nil {
		t.Fatal("origin with query accepted")
	}
	slowServer := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) { <-request.Context().Done() }))
	defer slowServer.Close()
	fetcher = testHTTPSystemUpdateFetcher(slowServer, publicKey)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := fetcher.Discover(ctx)
	if err == nil || (!errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded")) {
		t.Fatalf("timeout error = %v", err)
	}
}
