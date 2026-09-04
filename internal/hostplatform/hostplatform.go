// Package hostplatform identifies the Linux package ecosystem and verifies the
// host primitives the agent relies on. It treats os-release as untrusted audit
// metadata and routing evidence: callers never source it as shell code and a
// distro name is never authorization by itself.
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

type SecurityPolicyState string

const (
	SecurityPolicyInactive   SecurityPolicyState = "inactive"
	SecurityPolicyPermissive SecurityPolicyState = "selinux-permissive"
	SecurityPolicyEnforcing  SecurityPolicyState = "selinux-enforcing"
)

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
	ExecutablePresent   func(string) (bool, error)
	LookPath            func(string) (string, error)
	ValidateExecutable  func(string, string) error
	SystemdReady        func(string) error
	SecurityPolicyState func() (SecurityPolicyState, error)
	DNFSecurityReady    func(string) error
	Architecture        func() (string, error)
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

// DetectWith treats os-release family tokens as routing hints, never as a
// product allowlist. Unambiguous family evidence selects that family's trusted
// toolchain. Unknown or conflicting evidence falls back to capability
// discovery and is accepted only when exactly one supported toolchain is
// complete. Every present candidate package-manager executable is validated
// before routing, including safe foreign tools that are ignored for selection.
func DetectWith(data []byte, probe Probe) (Profile, error) {
	profile, err := detectWith(data, probe)
	if err != nil {
		// Every refusal below is a property of the host, not of the moment,
		// except the one the readiness probe marks as the boot condition. The
		// tag is added here, once, so a caller can ask "not yet or no?" without
		// this function's own messages changing.
		// Asagidaki her ret, ana degil makineye ait bir olgudur; tek istisna
		// hazirlik yoklamasinin acilis durumu diye isaretledigidir.
		return Profile{}, unsupported(err)
	}
	return profile, nil
}

func detectWith(data []byte, probe Probe) (Profile, error) {
	release, err := ParseOSRelease(data)
	if err != nil {
		return Profile{}, err
	}
	if probe.ExecutablePresent == nil || probe.LookPath == nil ||
		probe.ValidateExecutable == nil || probe.SystemdReady == nil ||
		probe.SecurityPolicyState == nil || probe.DNFSecurityReady == nil ||
		probe.Architecture == nil {
		return Profile{}, fmt.Errorf("host platform probe is incomplete")
	}
	systemctl, present, err := inspectExecutable("systemctl", probe)
	if err != nil {
		return Profile{}, fmt.Errorf("inspect systemd executable: %w", err)
	}
	if !present {
		return Profile{}, fmt.Errorf("systemd requires systemctl")
	}
	timeout, present, err := inspectExecutable("timeout", probe)
	if err != nil {
		return Profile{}, fmt.Errorf("inspect bounded-command executable: %w", err)
	}
	if !present {
		return Profile{}, fmt.Errorf("host platform requires timeout")
	}
	if err := probe.SystemdReady(systemctl); err != nil {
		return Profile{}, fmt.Errorf("systemd is not ready: %w", err)
	}
	presentPackageTools, err := validatePresentPackageTools(probe)
	if err != nil {
		return Profile{}, err
	}
	family, hasUnambiguousEvidence := familyForRelease(release)
	manager := PackageManager("")
	executables := map[string]string(nil)
	if hasUnambiguousEvidence {
		manager, executables, err = verifyFamilyToolchain(family, presentPackageTools)
	} else {
		family, manager, executables, err = discoverUniqueFamilyToolchain(presentPackageTools)
	}
	if err != nil {
		return Profile{}, err
	}
	if manager == PackageManagerDNF {
		for _, binary := range []string{"restorecon", "matchpathcon", "getenforce"} {
			path, present, inspectErr := inspectExecutable(binary, probe)
			if inspectErr != nil {
				return Profile{}, fmt.Errorf("inspect DNF security-policy executable: %w", inspectErr)
			}
			if !present {
				return Profile{}, fmt.Errorf("DNF preview requires %s at its fixed vendor path", binary)
			}
			executables[binary] = path
		}
	}
	executables["systemctl"] = systemctl
	executables["timeout"] = timeout
	securityState, err := probe.SecurityPolicyState()
	if err != nil {
		return Profile{}, fmt.Errorf("inspect host security policy: %w", err)
	}
	if err := verifyProfileSecurityPolicy(manager, securityState); err != nil {
		return Profile{}, err
	}
	if manager == PackageManagerDNF {
		if err := probe.DNFSecurityReady(executables["getenforce"]); err != nil {
			return Profile{}, fmt.Errorf("DNF preview security-policy proof failed: %w", err)
		}
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

func verifyProfileSecurityPolicy(manager PackageManager, state SecurityPolicyState) error {
	switch manager {
	case PackageManagerAPT, PackageManagerPacman:
		if state != SecurityPolicyInactive {
			return fmt.Errorf("%s mutations require inactive SELinux; live state is %s", manager, state)
		}
		return nil
	case PackageManagerDNF:
		if state != SecurityPolicyEnforcing {
			return fmt.Errorf("DNF preview requires SELinux enforcing; live state is %s", state)
		}
		return nil
	default:
		return fmt.Errorf("unsupported package manager %q for security-policy proof", manager)
	}
}

type familyToolchainCandidate struct {
	family      DistroFamily
	manager     PackageManager
	executables map[string]string
}

var candidatePackageTools = []string{
	"apt-get",
	"apt-cache",
	"dpkg-query",
	"pacman",
	"dnf",
	"rpm",
}

func inspectExecutable(name string, probe Probe) (string, bool, error) {
	present, err := probe.ExecutablePresent(name)
	if err != nil {
		return "", false, fmt.Errorf("determine whether %s is present: %w", name, err)
	}
	if !present {
		return "", false, nil
	}
	path, err := probe.LookPath(name)
	if err != nil {
		return "", false, fmt.Errorf("resolve present %s executable: %w", name, err)
	}
	if strings.TrimSpace(path) == "" {
		return "", false, fmt.Errorf("resolve present %s executable: empty path", name)
	}
	if err := probe.ValidateExecutable(name, path); err != nil {
		return "", false, fmt.Errorf("untrusted present %s executable: %w", name, err)
	}
	return path, true, nil
}

func validatePresentPackageTools(probe Probe) (map[string]string, error) {
	executables := make(map[string]string, len(candidatePackageTools))
	for _, binary := range candidatePackageTools {
		path, present, err := inspectExecutable(binary, probe)
		if err != nil {
			return nil, fmt.Errorf("inspect package-manager candidate: %w", err)
		}
		if present {
			executables[binary] = path
		}
	}
	return executables, nil
}

func verifyFamilyToolchain(
	family DistroFamily,
	present map[string]string,
) (PackageManager, map[string]string, error) {
	manager, required := familyRequirements(family)
	executables := make(map[string]string, len(required)+1)
	for _, binary := range required {
		path, ok := present[binary]
		if !ok {
			return "", nil, fmt.Errorf("%s family requires %s at its fixed vendor path", family, binary)
		}
		executables[binary] = path
	}
	return manager, executables, nil
}

func discoverUniqueFamilyToolchain(
	present map[string]string,
) (DistroFamily, PackageManager, map[string]string, error) {
	var candidates []familyToolchainCandidate
	for _, family := range []DistroFamily{
		DistroFamilyDebian,
		DistroFamilyRHEL,
		DistroFamilyArch,
	} {
		manager, executables, err := verifyFamilyToolchain(family, present)
		if err != nil {
			continue
		}
		candidates = append(candidates, familyToolchainCandidate{
			family: family, manager: manager, executables: executables,
		})
	}
	if len(candidates) == 0 {
		return "", "", nil, fmt.Errorf(
			"host has no complete trusted apt, dnf, or pacman toolchain",
		)
	}
	if len(candidates) > 1 {
		managers := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			managers = append(managers, string(candidate.manager))
		}
		sort.Strings(managers)
		return "", "", nil, fmt.Errorf(
			"host package-manager capability is ambiguous: %s toolchains are complete",
			strings.Join(managers, ", "),
		)
	}
	candidate := candidates[0]
	return candidate.family, candidate.manager, candidate.executables, nil
}

func canonicalArchitecture(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "amd64", "x86_64":
		return "amd64", nil
	case "arm64", "aarch64":
		return "arm64", nil
	}
	return "", fmt.Errorf("unsupported host architecture %q", value)
}

func familyForRelease(release OSRelease) (DistroFamily, bool) {
	evidence := make(map[DistroFamily]struct{})
	for _, token := range append([]string{release.ID}, release.IDLike...) {
		if family := familyForToken(token); family != "" {
			evidence[family] = struct{}{}
		}
	}
	if len(evidence) != 1 {
		return "", false
	}
	for family := range evidence {
		return family, true
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
