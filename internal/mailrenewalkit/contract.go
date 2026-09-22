// Package mailrenewalkit binds independent mail code and native scheduling files.
// A valid artifact is not permission to install, migrate or enable it.
package mailrenewalkit

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"github.com/alicelik/celikpanel/internal/mailtlsartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

const Schema = "celikpanel-mail-renewal-runtime/v1"
const ManifestName = "runtime.manifest"
const InstalledRoot = "/usr/libexec/celikpanel/mail-renewal"
const BinaryName = "renew"
const ServiceName = "celikpanel-mail-renewal.service"
const TimerName = "celikpanel-mail-renewal.timer"
const HookName = "celikpanel-mail-host-cert"
const MaxBinarySize = 128 << 20

//go:embed renewal.service
var serviceTemplate string

//go:embed renewal.timer
var timerTemplate string

//go:embed deploy-hook
var hookTemplate string

type Manifest struct {
	Schema        string `json:"schema"`
	Generation    string `json:"generation"`
	LedgerVersion int    `json:"ledger_version"`
	PlanVersion   int    `json:"plan_version"`
	ReceiptSchema string `json:"receipt_schema"`
	BinarySHA256  string `json:"binary_sha256"`
	ServiceSHA256 string `json:"service_sha256"`
	TimerSHA256   string `json:"timer_sha256"`
	HookSHA256    string `json:"hook_sha256"`
}

func Digest(raw []byte) string { v := sha256.Sum256(raw); return hex.EncodeToString(v[:]) }
func validDigest(s string) bool {
	v, e := hex.DecodeString(s)
	return e == nil && len(v) == 32 && strings.ToLower(s) == s
}
func generation(binary string) string {
	return Digest([]byte(Schema + "\n" + binary + "\n" + Digest([]byte(serviceTemplate)) + "\n" + Digest([]byte(timerTemplate)) + "\n" + Digest([]byte(hookTemplate)) + "\n"))
}
func render(template, id string) []byte {
	return []byte(strings.ReplaceAll(template, "@RENEW@", InstalledRoot+"/"+id+"/"+BinaryName))
}
func Payload(binary []byte) (Manifest, map[string][]byte, error) {
	if servicemutationledger.Version != 1 || mailtlsartifact.Version != 1 || mailhostartifact.ReceiptSchema != "mail-host-certificate-receipt/v1" {
		return Manifest{}, nil, errors.New("mail evidence versions require an explicit runtime transition")
	}
	if len(binary) == 0 || len(binary) > MaxBinarySize {
		return Manifest{}, nil, errors.New("invalid mail renewal binary size")
	}
	digest := Digest(binary)
	id := generation(digest)
	files := map[string][]byte{BinaryName: binary, ServiceName: render(serviceTemplate, id), TimerName: []byte(timerTemplate), HookName: render(hookTemplate, id)}
	m := Manifest{Schema: Schema, Generation: id, LedgerVersion: 1, PlanVersion: 1, ReceiptSchema: mailhostartifact.ReceiptSchema, BinarySHA256: digest, ServiceSHA256: Digest(files[ServiceName]), TimerSHA256: Digest(files[TimerName]), HookSHA256: Digest(files[HookName])}
	return m, files, nil
}
func validate(m Manifest) error {
	if m.Schema != Schema || m.LedgerVersion != 1 || m.PlanVersion != 1 || m.ReceiptSchema != "mail-host-certificate-receipt/v1" || !validDigest(m.BinarySHA256) || m.Generation != generation(m.BinarySHA256) || m.ServiceSHA256 != Digest(render(serviceTemplate, m.Generation)) || m.TimerSHA256 != Digest([]byte(timerTemplate)) || m.HookSHA256 != Digest(render(hookTemplate, m.Generation)) {
		return errors.New("unsupported or inconsistent mail renewal runtime")
	}
	return nil
}
func Encode(m Manifest) ([]byte, error) {
	if e := validate(m); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(m)
	return append(raw, '\n'), e
}
func Parse(raw []byte) (Manifest, error) {
	var m Manifest
	if len(raw) == 0 || len(raw) > 4096 {
		return m, errors.New("invalid mail runtime manifest size")
	}
	if e := json.Unmarshal(raw, &m); e != nil {
		return m, e
	}
	canonical, e := Encode(m)
	if e != nil {
		return m, e
	}
	if !bytes.Equal(raw, canonical) {
		return m, errors.New("noncanonical mail runtime manifest")
	}
	return m, nil
}
func Verify(raw []byte, files map[string][]byte) (Manifest, error) {
	m, e := Parse(raw)
	if e != nil {
		return m, e
	}
	if len(files) != 4 {
		return m, errors.New("mail runtime inventory differs")
	}
	expected, payload, e := Payload(files[BinaryName])
	if e != nil {
		return m, e
	}
	if expected != m {
		return m, errors.New("mail renewal binary differs")
	}
	for name, want := range payload {
		if !bytes.Equal(files[name], want) {
			return m, errors.New("mail runtime file differs")
		}
	}
	return m, nil
}

// ServiceGeneration recognizes only the exact supported independent unit.
func ServiceGeneration(raw []byte) (string, error) {
	prefix := InstalledRoot + "/"
	at := bytes.Index(raw, []byte(prefix))
	if at < 0 || len(raw) < at+len(prefix)+64 {
		return "", errors.New("unsupported mail runtime service")
	}
	id := string(raw[at+len(prefix) : at+len(prefix)+64])
	if !validDigest(id) || !bytes.Equal(raw, render(serviceTemplate, id)) {
		return "", errors.New("unsupported mail runtime service")
	}
	return id, nil
}

// LegacyHook is recognized for compatibility only, not independent enrollment.
//
//go:embed legacy-deploy-hook
var legacyHook []byte

func LegacyHook() []byte { return bytes.Clone(legacyHook) }

func HookGeneration(raw []byte) (string, error) {
	prefix := InstalledRoot + "/"
	at := bytes.Index(raw, []byte(prefix))
	if at < 0 || len(raw) < at+len(prefix)+64 {
		return "", errors.New("unsupported mail runtime hook")
	}
	id := string(raw[at+len(prefix) : at+len(prefix)+64])
	if !validDigest(id) || !bytes.Equal(raw, render(hookTemplate, id)) {
		return "", errors.New("unsupported mail runtime hook")
	}
	return id, nil
}
