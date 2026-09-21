package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

const ownerViewLifetime = 30 * time.Minute

// The view carries no mutable plan, database handle, RPC client or shell callback.
// It can only read the single request authorized by the native owner command.
type ownerViewOptions struct {
	requestID string
	port      int
	lang      string
}

func parseOwnerView(args []string) (ownerViewOptions, bool) {
	o := ownerViewOptions{port: 2084, lang: "en"}
	if len(args) < 3 || args[0] != "view" {
		return o, false
	}
	seen := map[string]bool{}
	for i := 1; i < len(args); i += 2 {
		if i+1 >= len(args) || seen[args[i]] {
			return o, false
		}
		seen[args[i]] = true
		switch args[i] {
		case "--request-id":
			if !recoveryobs.ValidRequestID(args[i+1]) {
				return o, false
			}
			o.requestID = args[i+1]
		case "--port":
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1024 || n > 65535 || strconv.Itoa(n) != args[i+1] {
				return o, false
			}
			o.port = n
		case "--lang":
			if args[i+1] != "en" && args[i+1] != "tr" {
				return o, false
			}
			o.lang = args[i+1]
		default:
			return o, false
		}
	}
	return o, o.requestID != ""
}

//go:embed owner_view.html
var ownerViewHTML string

//go:embed owner_view.js
var ownerViewJS string

//go:embed owner_view.css
var ownerViewCSS string

func ownerViewHandler(o ownerViewOptions, secretHash [32]byte, deadline time.Time, now func() time.Time, read func(string) recoveryobs.Status) http.Handler {
	host := "127.0.0.1:" + strconv.Itoa(o.port)
	origin := "http://" + host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		if r.Host != host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if !now().Before(deadline) {
			w.WriteHeader(http.StatusGone)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RawQuery != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, strings.Replace(ownerViewHTML, "LANGUAGE", o.lang, 1))
		case "/view.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			io.WriteString(w, ownerViewJS)
		case "/view.css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			io.WriteString(w, ownerViewCSS)
		case "/status":
			headers := r.Header.Values("Authorization")
			if len(headers) != 1 || !strings.HasPrefix(headers[0], "Bearer ") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(headers[0], "Bearer ")
			raw, err := hex.DecodeString(token)
			if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			digest := sha256.Sum256(raw)
			if subtle.ConstantTimeCompare(digest[:], secretHash[:]) != 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			status := read(o.requestID)
			if status.Schema != recoveryobs.StatusSchema || status.RequestID != o.requestID || status.Observation != "known" {
				status = unavailableStatus(o.requestID)
			}
			var en, tr bytes.Buffer
			_ = writeStatus(&en, "en", status)
			_ = writeStatus(&tr, "tr", status)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(struct {
				Status    recoveryobs.Status `json:"status"`
				EN        string             `json:"en"`
				TR        string             `json:"tr"`
				ExpiresAt string             `json:"expires_at"`
			}{status, en.String(), tr.String(), deadline.UTC().Format(time.RFC3339)})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}
