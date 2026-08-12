// Package hostplatform identifies the Linux distribution family and verifies
// the host primitives the agent relies on. It treats os-release as untrusted
// data: callers never source it as shell code.
package hostplatform

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type DistroFamily string

const (
	DistroFamilyDebian DistroFamily = "debian"
	DistroFamilyRHEL   DistroFamily = "rhel"
	DistroFamilyArch   DistroFamily = "arch"
)

type PackageManager string

const (
	PackageManagerAPT    PackageManager = "apt"
	PackageManagerDNF    PackageManager = "dnf"
	PackageManagerPacman PackageManager = "pacman"
)

type ServiceManager string

const ServiceManagerSystemd ServiceManager = "systemd"

type Profile struct {
	DistroFamily   DistroFamily
	PackageManager PackageManager
	ServiceManager ServiceManager
	Executables    map[string]string
	ID             string
	IDLike         []string
	Version        string
	Codename       string
	Arch           string
}

type OSRelease struct {
	ID       string
	IDLike   []string
	Version  string
	Codename string
}

// Probe contains the host operations used after parsing os-release. Keeping
// these injectable makes failure modes testable without weakening production
// detection.
type Probe struct {
	LookPath           func(string) (string, error)
	ValidateExecutable func(string, string) error
	SystemdReady       func(string) error
	Architecture       func() (string, error)
}

var (
	osReleaseKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	idToken      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	versionToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
)

// ParseOSRelease parses the standard assignment format without evaluating any
// shell syntax. Duplicate keys and malformed assignments fail closed.
func ParseOSRelease(data []byte) (OSRelease, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return OSRelease{}, fmt.Errorf("os-release contains a NUL byte")
	}
	values := make(map[string]string)
	for lineNo, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSuffix(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line != strings.TrimLeft(line, " \t") {
			return OSRelease{}, fmt.Errorf("os-release line %d has leading whitespace", lineNo+1)
		}
		key, encoded, ok := strings.Cut(line, "=")
		if !ok || !osReleaseKey.MatchString(key) {
			return OSRelease{}, fmt.Errorf("os-release line %d is not a valid assignment", lineNo+1)
		}
		if _, duplicate := values[key]; duplicate {
			return OSRelease{}, fmt.Errorf("os-release key %s is duplicated", key)
		}
		value, err := decodeOSReleaseValue(encoded)
		if err != nil {
			return OSRelease{}, fmt.Errorf("os-release key %s: %w", key, err)
		}
		values[key] = value
	}

	release := OSRelease{
		ID:       strings.ToLower(values["ID"]),
		Version:  values["VERSION_ID"],
		Codename: strings.ToLower(values["VERSION_CODENAME"]),
	}
	if !idToken.MatchString(release.ID) {
		return OSRelease{}, fmt.Errorf("os-release ID is missing or invalid")
	}
	if release.Version != "" && !versionToken.MatchString(release.Version) {
		return OSRelease{}, fmt.Errorf("os-release VERSION_ID is invalid")
	}
	if release.Codename != "" && !idToken.MatchString(release.Codename) {
		return OSRelease{}, fmt.Errorf("os-release VERSION_CODENAME is invalid")
	}
	for _, token := range strings.Fields(strings.ToLower(values["ID_LIKE"])) {
		if !idToken.MatchString(token) {
			return OSRelease{}, fmt.Errorf("os-release ID_LIKE contains invalid token %q", token)
		}
		release.IDLike = append(release.IDLike, token)
	}
	return release, nil
}

func decodeOSReleaseValue(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	if encoded[0] != '\'' && encoded[0] != '"' {
		if strings.ContainsAny(encoded, " \t\r\\\"'`$;") {
			return "", fmt.Errorf("unsafe unquoted value")
		}
		return encoded, nil
	}
	quote := encoded[0]
	if len(encoded) < 2 || encoded[len(encoded)-1] != quote {
		return "", fmt.Errorf("unterminated quoted value")
	}
	body := encoded[1 : len(encoded)-1]
	if quote == '\'' {
		if strings.ContainsRune(body, '\'') {
			return "", fmt.Errorf("invalid single-quoted value")
		}
		return body, nil
	}
	var out strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			if body[i] == '"' {
				return "", fmt.Errorf("unescaped quote")
			}
			out.WriteByte(body[i])
			continue
		}
		i++
		if i == len(body) || !strings.ContainsRune(`\\\"$`+"`", rune(body[i])) {
			return "", fmt.Errorf("invalid escape")
		}
		out.WriteByte(body[i])
	}
	return out.String(), nil
}

// DetectWith identifies the family from release data first, then verifies the
// exact executables for that family. Foreign package managers are irrelevant.
func DetectWith(data []byte, probe Probe) (Profile, error) {
	release, err := ParseOSRelease(data)
	if err != nil {
		return Profile{}, err
	}
	family, err := familyForRelease(release)
	if err != nil {
		return Profile{}, err
	}
	if !isRecognizedDistroID(release.ID) {
		return Profile{}, fmt.Errorf("distribution %s has compatible %s family evidence but its distro ID is unverified", release.ID, family)
	}
	manager, required := familyRequirements(family)
	if probe.LookPath == nil || probe.ValidateExecutable == nil || probe.SystemdReady == nil || probe.Architecture == nil {
		return Profile{}, fmt.Errorf("host platform probe is incomplete")
	}
	executables := make(map[string]string, len(required)+1)
	for _, binary := range append(required, "systemctl") {
		path, err := probe.LookPath(binary)
		if err != nil {
			return Profile{}, fmt.Errorf("%s family requires %s: %w", family, binary, err)
		}
		if err := probe.ValidateExecutable(binary, path); err != nil {
			return Profile{}, fmt.Errorf("%s family has untrusted %s executable: %w", family, binary, err)
		}
		executables[binary] = path
	}
	if err := probe.SystemdReady(executables["systemctl"]); err != nil {
		return Profile{}, fmt.Errorf("systemd is not ready: %w", err)
	}
	arch, err := probe.Architecture()
	if err != nil {
		return Profile{}, fmt.Errorf("detect architecture: %w", err)
	}
	arch, err = canonicalArchitecture(arch)
	if err != nil {
		return Profile{}, err
	}
	return Profile{
		DistroFamily: family, PackageManager: manager,
		ServiceManager: ServiceManagerSystemd, Executables: executables,
		ID: release.ID, IDLike: append([]string(nil), release.IDLike...),
		Version: release.Version, Codename: release.Codename, Arch: arch,
	}, nil
}

func isRecognizedDistroID(id string) bool {
	switch id {
	case "debian", "ubuntu", "rhel", "almalinux", "rocky", "centos", "fedora", "cloudlinux", "arch":
		return true
	default:
		return false
	}
}

func canonicalArchitecture(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "amd64", "x86_64":
		return "amd64", nil
	case "arm64", "aarch64":
		return "arm64", nil
	case "386", "i386", "i486", "i586", "i686":
		return "386", nil
	case "arm", "armv7", "armv7l", "armhf":
		return "arm", nil
	}
	if !idToken.MatchString(value) {
		return "", fmt.Errorf("host architecture is invalid")
	}
	return value, nil
}

func familyForRelease(release OSRelease) (DistroFamily, error) {
	if reason, rejected := rejectedDistribution(release.ID); rejected {
		return "", fmt.Errorf("distribution %s is unsupported: %s", release.ID, reason)
	}
	evidence := make(map[DistroFamily]struct{})
	for _, token := range append([]string{release.ID}, release.IDLike...) {
		if reason, rejected := rejectedDistribution(token); rejected {
			return "", fmt.Errorf("distribution family evidence %s is unsupported: %s", token, reason)
		}
		if family := familyForToken(token); family != "" {
			evidence[family] = struct{}{}
		}
	}
	if len(evidence) == 0 {
		return "", fmt.Errorf("distribution %s does not identify a supported family", release.ID)
	}
	if len(evidence) > 1 {
		families := make([]string, 0, len(evidence))
		for family := range evidence {
			families = append(families, string(family))
		}
		sort.Strings(families)
		return "", fmt.Errorf("os-release has conflicting family evidence: %s", strings.Join(families, ", "))
	}
	for family := range evidence {
		return family, nil
	}
	panic("unreachable")
}

func familyForToken(token string) DistroFamily {
	switch token {
	case "debian", "ubuntu":
		return DistroFamilyDebian
	case "rhel", "fedora", "centos", "almalinux", "rocky", "rocky-linux", "cloudlinux":
		return DistroFamilyRHEL
	case "arch":
		return DistroFamilyArch
	default:
		return ""
	}
}

func rejectedDistribution(token string) (string, bool) {
	switch token {
	case "kali":
		return "Kali is not a managed-server target", true
	case "suse", "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles":
		return "the SUSE family is not supported", true
	case "alpine":
		return "the Alpine family is not supported", true
	case "nixos":
		return "NixOS is not supported", true
	default:
		return "", false
	}
}

func familyRequirements(family DistroFamily) (PackageManager, []string) {
	switch family {
	case DistroFamilyDebian:
		return PackageManagerAPT, []string{"apt-get", "apt-cache", "dpkg-query"}
	case DistroFamilyRHEL:
		return PackageManagerDNF, []string{"dnf", "rpm"}
	case DistroFamilyArch:
		return PackageManagerPacman, []string{"pacman"}
	default:
		panic("unsupported family")
	}
}
