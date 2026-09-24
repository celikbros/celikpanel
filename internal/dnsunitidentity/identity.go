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
