package dnsunitidentity

import (
	"errors"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"
)

// Identity is systemd's parsed service fragment and command identity.
type Identity struct {
	ID            string
	Names         []string
	FragmentPath  string
	DropInPaths   []string
	SourcePath    string
	Transient     string
	ExecStartPath string
	ExecStartArgv string
}

func Parse(output string) (Identity, error) {
	const expectedProperties = 7
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return Identity{}, errors.New("systemctl returned a malformed DNS unit identity")
		}
		switch key {
		case "Id", "Names", "FragmentPath", "DropInPaths", "SourcePath", "Transient", "ExecStart":
		default:
			return Identity{}, errors.New("systemctl returned an unexpected DNS unit identity property")
		}
		if _, duplicate := values[key]; duplicate {
			return Identity{}, errors.New("systemctl returned an ambiguous DNS unit identity")
		}
		values[key] = value
	}
	if len(values) != expectedProperties {
		return Identity{}, errors.New("systemctl returned incomplete DNS unit identity")
	}
	parseNames := func(property string, allowEmpty bool) ([]string, error) {
		raw := values[property]
		if raw == "" && allowEmpty {
			return nil, nil
		}
		fields := strings.Fields(raw)
		if len(fields) == 0 || strings.Join(fields, " ") != raw {
			return nil, fmt.Errorf("systemctl returned non-canonical %s", property)
		}
		sort.Strings(fields)
		for index := 1; index < len(fields); index++ {
			if fields[index] == fields[index-1] {
				return nil, fmt.Errorf("systemctl returned duplicate %s", property)
			}
		}
		return fields, nil
	}
	names, err := parseNames("Names", false)
	if err != nil {
		return Identity{}, err
	}
	dropIns, err := parseNames("DropInPaths", true)
	if err != nil {
		return Identity{}, err
	}
	execPath, execArgv, err := parseExecStart(values["ExecStart"])
	if err != nil {
		return Identity{}, err
	}
	if values["Id"] == "" || values["FragmentPath"] == "" ||
		values["Transient"] == "" {
		return Identity{}, errors.New("systemctl returned an empty DNS unit identity field")
	}
	return Identity{
		ID: values["Id"], Names: names, FragmentPath: values["FragmentPath"],
		DropInPaths: dropIns, SourcePath: values["SourcePath"],
		Transient: values["Transient"], ExecStartPath: execPath,
		ExecStartArgv: execArgv,
	}, nil
}

func parseExecStart(value string) (string, string, error) {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") ||
		strings.Count(value, "{") != 1 || strings.Count(value, "}") != 1 ||
		!strings.HasPrefix(value, "{ ") || !strings.HasSuffix(value, " }") {
		return "", "", errors.New("systemctl returned a non-canonical ExecStart")
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(value, "{ "), " }")
	parts := strings.Split(inner, " ; ")
	fields := map[string]string{}
	for _, part := range parts {
		key, candidate, found := strings.Cut(part, "=")
		if !found || key == "" || candidate == "" {
			return "", "", errors.New("systemctl returned a malformed ExecStart")
		}
		switch key {
		case "path", "argv[]", "ignore_errors":
			if _, duplicate := fields[key]; duplicate {
				return "", "", errors.New("systemctl returned an ambiguous ExecStart")
			}
			fields[key] = candidate
		}
	}
	if len(fields) != 3 || fields["ignore_errors"] != "no" {
		return "", "", errors.New("systemctl returned an unsafe ExecStart")
	}
	executable := fields["path"]
	if !path.IsAbs(executable) || path.Clean(executable) != executable ||
		(fields["argv[]"] != executable &&
			!strings.HasPrefix(fields["argv[]"], executable+" ")) {
		return "", "", errors.New("systemctl returned a non-canonical ExecStart command")
	}
	return executable, fields["argv[]"], nil
}

func ValidateAPTBINDVendorNamedIdentity(
	named Identity,
	aliasEnabled bool,
) error {
	expectedNames := []string{"named.service"}
	if aliasEnabled {
		expectedNames = []string{"bind9.service", "named.service"}
	}
	if named.ID != "named.service" ||
		!reflect.DeepEqual(named.Names, expectedNames) ||
		named.FragmentPath != "/usr/lib/systemd/system/named.service" ||
		len(named.DropInPaths) != 0 || named.SourcePath != "" ||
		named.Transient != "no" || named.ExecStartPath != "/usr/sbin/named" ||
		named.ExecStartArgv != "/usr/sbin/named -f $OPTIONS" {
		return errors.New("named.service does not resolve to the exact APT vendor BIND identity")
	}
	return nil
}

func ValidateAPTBINDVendorAliasIdentity(named, alias Identity) error {
	if err := ValidateAPTBINDVendorNamedIdentity(named, true); err != nil {
		return err
	}
	if err := ValidateAPTBINDVendorNamedIdentity(alias, true); err != nil ||
		!reflect.DeepEqual(named, alias) {
		return errors.New("BIND service aliases do not resolve to the exact vendor named.service identity")
	}
	return nil
}

func ValidatePacmanBINDVendorIdentity(named Identity) error {
	if named.ID != "named.service" ||
		!reflect.DeepEqual(named.Names, []string{"named.service"}) ||
		named.FragmentPath != "/usr/lib/systemd/system/named.service" ||
		len(named.DropInPaths) != 0 || named.SourcePath != "" ||
		named.Transient != "no" || named.ExecStartPath != "/usr/bin/named" ||
		named.ExecStartArgv != "/usr/bin/named -f -u named" {
		return errors.New("named.service does not resolve to the exact vendor BIND identity")
	}
	return nil
}

// ValidateAPTPDNSVendorIdentity is the exact installed PowerDNS unit
// identity accepted by the APT adapter. Vendor file bytes are a separate
// package-provenance proof.
func ValidateAPTPDNSVendorIdentity(identity Identity) error {
	const execArgv = "/usr/sbin/pdns_server --guardian=no --daemon=no --disable-syslog --log-timestamp=no --write-pid=no"
	if identity.ID != "pdns.service" ||
		!reflect.DeepEqual(identity.Names, []string{"pdns.service"}) ||
		identity.FragmentPath != "/usr/lib/systemd/system/pdns.service" ||
		len(identity.DropInPaths) != 0 || identity.SourcePath != "" ||
		identity.Transient != "no" ||
		identity.ExecStartPath != "/usr/sbin/pdns_server" ||
		identity.ExecStartArgv != execArgv {
		return errors.New("pdns.service does not resolve to the exact certified vendor identity")
	}
	return nil
}

// TargetState is the systemd load class of a DNS switch target unit.
type TargetState uint8

const (
	// TargetLoaded carries the strict Identity that Parse accepts.
	TargetLoaded TargetState = iota + 1
	// TargetPersistentMask is a persistent mask under /etc/systemd/system;
	// systemd exposes no service command for it.
	TargetPersistentMask
	// TargetAbsent is a unit name systemd cannot load (not-found).
	TargetAbsent
)

// TargetObservation is one parsed reading of a DNS switch target unit. A
// loaded unit carries the strict Identity; a persistently masked or absent
// unit is represented as an explicit, typed never-started observation instead
// of an incomplete identity. The type only records what systemd reported: a
// caller must prove from its own evidence that such a target may be in that
// state, and must separately prove the mask link and the stopped runtime.
type TargetObservation struct {
	State        TargetState
	ID           string
	FragmentPath string
	Identity     Identity
}

// TargetObservationProperties are the properties ParseTargetObservation reads.
const TargetObservationProperties = "Id,Names,LoadState,UnitFileState,FragmentPath,DropInPaths,SourcePath,Transient,ExecStart"

// ParseTargetObservation classifies a systemctl show reading of exactly
// TargetObservationProperties. A loaded unit is parsed by the unchanged Parse
// and must carry all seven identity properties. A masked unit is accepted only
// as a persistent mask whose fragment is /etc/systemd/system/<Id>; a runtime
// mask ("masked-runtime" or a /run fragment) is refused. A not-found unit must
// have an empty unit-file state and no fragment. Neither may report drop-ins,
// a source path, a transient unit or a service command.
func ParseTargetObservation(output string) (TargetObservation, error) {
	values := map[string]string{}
	identityLines := make([]string, 0, 7)
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return TargetObservation{}, errors.New("systemctl returned a malformed DNS target unit observation")
		}
		switch key {
		case "Id", "Names", "FragmentPath", "DropInPaths", "SourcePath", "Transient", "ExecStart":
			identityLines = append(identityLines, line)
		case "LoadState", "UnitFileState":
		default:
			return TargetObservation{}, errors.New("systemctl returned an unexpected DNS target unit property")
		}
		if _, duplicate := values[key]; duplicate {
			return TargetObservation{}, errors.New("systemctl returned an ambiguous DNS target unit observation")
		}
		values[key] = value
	}
	for _, key := range []string{"Id", "Names", "LoadState", "UnitFileState"} {
		if _, present := values[key]; !present {
			return TargetObservation{}, errors.New("systemctl returned an incomplete DNS target unit observation")
		}
	}
	id := values["Id"]
	if values["LoadState"] == "loaded" {
		identity, err := Parse(strings.Join(identityLines, "\n"))
		if err != nil {
			return TargetObservation{}, err
		}
		if identity.ID != id {
			return TargetObservation{}, errors.New("systemctl returned an inconsistent DNS target unit identity")
		}
		return TargetObservation{State: TargetLoaded, ID: id, FragmentPath: identity.FragmentPath, Identity: identity}, nil
	}
	if id == "" || strings.ContainsAny(id, "/ \t") || values["Names"] != id {
		return TargetObservation{}, errors.New("systemctl returned a non-canonical never-started DNS target name")
	}
	for _, key := range []string{"DropInPaths", "SourcePath", "ExecStart"} {
		if values[key] != "" {
			return TargetObservation{}, fmt.Errorf("never-started DNS target unexpectedly reports %s", key)
		}
	}
	if transient, present := values["Transient"]; present && transient != "no" {
		return TargetObservation{}, errors.New("never-started DNS target is not a persistent unit")
	}
	switch values["LoadState"] {
	case "masked":
		if values["UnitFileState"] != "masked" || values["FragmentPath"] != "/etc/systemd/system/"+id {
			return TargetObservation{}, errors.New("masked DNS target is not a persistent /etc/systemd/system mask")
		}
		return TargetObservation{State: TargetPersistentMask, ID: id, FragmentPath: values["FragmentPath"]}, nil
	case "not-found":
		if values["UnitFileState"] != "" || values["FragmentPath"] != "" {
			return TargetObservation{}, errors.New("absent DNS target reports unit-file state or a fragment")
		}
		return TargetObservation{State: TargetAbsent, ID: id}, nil
	default:
		return TargetObservation{}, errors.New("DNS target unit is neither loaded, persistently masked nor absent")
	}
}
