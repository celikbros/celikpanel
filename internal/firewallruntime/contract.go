// Package firewallruntime defines the closed independent firewall artifact.
// Packaging grants no installation or service-start authority.
package firewallruntime

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/firewallpolicy"
)

const Schema = "celikpanel-firewall-runtime/v1"
const ManifestName = "runtime.manifest"
const InstalledRoot = "/usr/libexec/celikpanel/firewall"
const MaxBinarySize = 32 << 20

// The v1 template is part of the versioned producer/reader contract. Future
// changes require an explicit supported transition, not silent reinterpretation.
//
//go:embed firewall.service
var unitTemplate string

type Manifest struct {
	PolicyVersion int    `json:"policy_version"`
	Schema        string `json:"schema"`
	Generation    string `json:"generation"`
	BinarySHA256  string `json:"binary_sha256"`
	UnitSHA256    string `json:"unit_sha256"`
}

func Digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}
func generation(binary string) string {
	return Digest([]byte(Schema + "\n" + binary + "\n" + Digest([]byte(unitTemplate)) + "\n"))
}
func renderUnit(id string) []byte {
	return []byte(strings.ReplaceAll(unitTemplate, "@RESTORE@", InstalledRoot+"/"+id+"/restore"))
}

// Build uses the existing exact legacy/v2 reader; it does not change policy bytes.
func Build(binary []byte) (Manifest, []byte, error) {
	if firewallpolicy.Version != 2 {
		return Manifest{}, nil, errors.New("firewall reader version requires an explicit artifact transition")
	}
	if len(binary) == 0 || len(binary) > MaxBinarySize {
		return Manifest{}, nil, errors.New("invalid firewall binary size")
	}
	digest := Digest(binary)
	id := generation(digest)
	unit := renderUnit(id)
	return Manifest{PolicyVersion: 2, Schema: Schema, Generation: id, BinarySHA256: digest, UnitSHA256: Digest(unit)}, unit, nil
}
func Encode(m Manifest) ([]byte, error) {
	if err := validate(m); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(m)
	return append(raw, '\n'), err
}
func validate(m Manifest) error {
	if m.PolicyVersion != 2 || m.Schema != Schema || !validDigest(m.BinarySHA256) || m.Generation != generation(m.BinarySHA256) || m.UnitSHA256 != Digest(renderUnit(m.Generation)) {
		return errors.New("unsupported or inconsistent firewall runtime contract")
	}
	return nil
}
func Parse(raw []byte) (Manifest, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return Manifest{}, errors.New("invalid firewall manifest size")
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	canonical, err := Encode(m)
	if err != nil {
		return m, err
	}
	if !bytes.Equal(raw, canonical) {
		return m, errors.New("noncanonical firewall manifest")
	}
	return m, nil
}
func Verify(raw, binary, unit []byte) (Manifest, error) {
	m, err := Parse(raw)
	if err != nil {
		return m, err
	}
	if len(binary) == 0 || len(binary) > MaxBinarySize || Digest(binary) != m.BinarySHA256 || !bytes.Equal(unit, renderUnit(m.Generation)) {
		return m, errors.New("firewall runtime payload differs from manifest")
	}
	return m, nil
}

// UnitGeneration accepts only the exact v1 template and its bound helper path.
// Unknown future templates need an explicit reader transition.
func UnitGeneration(unit []byte) (string, error) {
	prefix := InstalledRoot + "/"
	at := bytes.Index(unit, []byte(prefix))
	if at < 0 || len(unit) < at+len(prefix)+64 {
		return "", errors.New("unsupported independent firewall unit")
	}
	id := string(unit[at+len(prefix) : at+len(prefix)+64])
	if !validDigest(id) || !bytes.Equal(unit, renderUnit(id)) {
		return "", errors.New("unsupported independent firewall unit")
	}
	return id, nil
}
