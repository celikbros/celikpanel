package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	systemUpdateHTTPTimeout      = 12 * time.Second
	systemUpdateLatestMaxSize    = 128
	systemUpdateSignatureMaxSize = ed25519.SignatureSize
)

type systemUpdateManifestFetcher interface {
	Discover(context.Context) (string, error)
	Fetch(context.Context, string, string, string) (systemUpdateManifest, error)
}

type httpSystemUpdateManifestFetcher struct {
	origin  string
	client  *http.Client
	loadKey func() (ed25519.PublicKey, error)
}

func newHTTPSystemUpdateManifestFetcher() *httpSystemUpdateManifestFetcher {
	return &httpSystemUpdateManifestFetcher{
		origin: systemUpdateOrigin,
		client: &http.Client{
			Timeout: systemUpdateHTTPTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		loadKey: func() (ed25519.PublicKey, error) {
			return loadSystemUpdatePublicKey(systemUpdateKeyPath)
		},
	}
}

func (fetcher *httpSystemUpdateManifestFetcher) Discover(ctx context.Context) (string, error) {
	raw, err := fetcher.fetchBytes(ctx, "/releases/latest.txt", systemUpdateLatestMaxSize)
	if err != nil {
		return "", fmt.Errorf("discover latest release: %w", err)
	}
	if len(raw) < 2 || raw[len(raw)-1] != '\n' || strings.Contains(string(raw[:len(raw)-1]), "\n") {
		return "", errors.New("latest release discovery is not one canonical line")
	}
	version := string(raw[:len(raw)-1])
	if _, err := parseSystemUpdateSemver(version); err != nil {
		return "", fmt.Errorf("latest release discovery is invalid: %w", err)
	}
	return version, nil
}

func (fetcher *httpSystemUpdateManifestFetcher) Fetch(
	ctx context.Context,
	version, platformOS, platformArch string,
) (systemUpdateManifest, error) {
	if _, err := parseSystemUpdateSemver(version); err != nil {
		return systemUpdateManifest{}, err
	}
	if platformOS != "linux" || (platformArch != "amd64" && platformArch != "arm64") {
		return systemUpdateManifest{}, errors.New("unsupported system-update platform")
	}
	base := "/releases/" + version + "/" + platformOS + "/" + platformArch + "/release-manifest-v2"
	raw, err := fetcher.fetchBytes(ctx, base, systemUpdateMaxManifestSize)
	if err != nil {
		return systemUpdateManifest{}, fmt.Errorf("fetch signed release manifest: %w", err)
	}
	signature, err := fetcher.fetchBytes(ctx, base+".sig", systemUpdateSignatureMaxSize)
	if err != nil {
		return systemUpdateManifest{}, fmt.Errorf("fetch release signature: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return systemUpdateManifest{}, errors.New("release signature length is invalid")
	}
	if fetcher.loadKey == nil {
		return systemUpdateManifest{}, errors.New("release public-key loader is unavailable")
	}
	publicKey, err := fetcher.loadKey()
	if err != nil {
		return systemUpdateManifest{}, fmt.Errorf("load release public key: %w", err)
	}
	if len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(publicKey, raw, signature) {
		return systemUpdateManifest{}, errors.New("release manifest signature is invalid")
	}
	manifest, err := parseCanonicalSystemUpdateManifest(raw)
	if err != nil {
		return systemUpdateManifest{}, err
	}
	if manifest.Version != version || manifest.OS != platformOS || manifest.Arch != platformArch {
		return systemUpdateManifest{}, errors.New("signed release manifest targets another release or platform")
	}
	return manifest, nil
}

func (fetcher *httpSystemUpdateManifestFetcher) fetchBytes(ctx context.Context, path string, maximum int64) ([]byte, error) {
	if fetcher == nil || fetcher.client == nil || maximum <= 0 || !strings.HasPrefix(path, "/") {
		return nil, errors.New("invalid fixed-origin fetch request")
	}
	origin, err := url.Parse(fetcher.origin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("invalid update origin")
	}
	target, err := url.Parse(fetcher.origin + path)
	if err != nil || target.Scheme != origin.Scheme || target.Host != origin.Host || target.User != nil {
		return nil, errors.New("update target escaped the fixed origin")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "CelikPanel-Agent-SystemUpdate/1")
	response, err := fetcher.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL == nil || response.Request.URL.Scheme != origin.Scheme || response.Request.URL.Host != origin.Host {
		return nil, errors.New("update response escaped the fixed origin")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update origin returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maximum {
		return nil, errors.New("update response exceeds its size limit")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maximum {
		return nil, errors.New("update response exceeds its size limit")
	}
	return raw, nil
}
