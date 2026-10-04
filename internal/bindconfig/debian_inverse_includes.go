package bindconfig

import (
	"errors"
	"fmt"
	"strings"
)

const (
	debianInverseDefaultZones = "/etc/bind/named.conf.default-zones"
	debianInverseRootHints    = "/etc/bind/named.conf.root-hints"
)

// DebianInverseMainLeaf accepts only comments, whitespace and exactly three
// top-level includes: options, local and one supported Debian leaf. It returns
// the selected leaf for the frozen evidence path, never for live fallback.
func DebianInverseMainLeaf(config string) (string, error) {
	counts := make(map[string]int)
	for index := 0; index < len(config); {
		if next, skipped, err := skipBINDTrivia(config, index); err != nil {
			return "", err
		} else if skipped {
			index = next
			continue
		}
		start := index
		if !bindMainIdentifierStart(config[index]) {
			return "", errors.New("Debian BIND main config contains an unexpected statement")
		}
		for index < len(config) && bindMainIdentifierPart(config[index]) {
			index++
		}
		if config[start:index] != "include" {
			return "", errors.New("Debian BIND main config contains a non-include statement")
		}
		for index < len(config) {
			if next, skipped, err := skipBINDTrivia(config, index); err != nil {
				return "", err
			} else if skipped {
				index = next
				continue
			}
			break
		}
		if index == len(config) || config[index] != '"' {
			return "", errors.New("Debian BIND main include has no canonical path")
		}
		next, path, err := readBINDString(config, index)
		if err != nil {
			return "", err
		}
		index = next
		for index < len(config) {
			if next, skipped, err := skipBINDTrivia(config, index); err != nil {
				return "", err
			} else if skipped {
				index = next
				continue
			}
			break
		}
		if index == len(config) || config[index] != ';' {
			return "", errors.New("Debian BIND main include lacks semicolon")
		}
		index++
		counts[path]++
	}
	for _, path := range []string{"/etc/bind/named.conf.options", "/etc/bind/named.conf.local"} {
		if counts[path] != 1 {
			return "", fmt.Errorf("Debian BIND main config lacks one exact include of %s", path)
		}
	}
	leaf := ""
	switch {
	case counts[debianInverseDefaultZones] == 1 && counts[debianInverseRootHints] == 0:
		leaf = debianInverseDefaultZones
	case counts[debianInverseRootHints] == 1 && counts[debianInverseDefaultZones] == 0:
		leaf = debianInverseRootHints
	default:
		return "", errors.New("Debian BIND main config must select exactly one supported leaf")
	}
	if len(counts) != 3 {
		return "", errors.New("Debian BIND main config has an extra include")
	}
	return leaf, nil
}

// VerifyDebianInverseMainIncludes retains the simple validation API.
func VerifyDebianInverseMainIncludes(config string) error {
	_, err := DebianInverseMainLeaf(config)
	return err
}

// VerifyDebianInverseNoIncludes refuses any active include token, including
// nested ones. Owner content remains untouched; this is only an admission rule.
func VerifyDebianInverseNoIncludes(config string) error {
	includes, err := debianInverseActiveIncludes(config)
	if err != nil {
		return err
	}
	if len(includes) != 0 {
		return errors.New("Debian BIND inverse leaf has an extra active include")
	}
	return nil
}

// VerifyDebianInverseManagedLeaf admits only the exact managed include in the
// post-write local file, with no additional active include at any depth.
func VerifyDebianInverseManagedLeaf(config, managedPath string) error {
	if err := VerifyExactZoneInclude(config, managedPath); err != nil {
		return err
	}
	includes, err := debianInverseActiveIncludes(config)
	if err != nil {
		return err
	}
	if len(includes) != 1 || includes[0] != managedPath {
		return errors.New("Debian BIND managed leaf has another active include")
	}
	return nil
}

func debianInverseActiveIncludes(config string) ([]string, error) {
	var includes []string
	for index := 0; index < len(config); {
		if next, skipped, err := skipBINDTrivia(config, index); err != nil {
			return nil, err
		} else if skipped {
			index = next
			continue
		}
		if config[index] == '"' {
			next, _, err := readBINDString(config, index)
			if err != nil {
				return nil, err
			}
			index = next
			continue
		}
		if !bindMainIdentifierStart(config[index]) {
			index++
			continue
		}
		start := index
		for index < len(config) && bindMainIdentifierPart(config[index]) {
			index++
		}
		if !strings.EqualFold(config[start:index], "include") {
			continue
		}
		for index < len(config) {
			if next, skipped, err := skipBINDTrivia(config, index); err != nil {
				return nil, err
			} else if skipped {
				index = next
				continue
			}
			break
		}
		if index == len(config) || config[index] != '"' {
			return nil, errors.New("Debian BIND leaf has non-canonical include")
		}
		next, path, err := readBINDString(config, index)
		if err != nil {
			return nil, err
		}
		index = next
		for index < len(config) {
			if next, skipped, err := skipBINDTrivia(config, index); err != nil {
				return nil, err
			} else if skipped {
				index = next
				continue
			}
			break
		}
		if index == len(config) || config[index] != ';' {
			return nil, errors.New("Debian BIND leaf include lacks semicolon")
		}
		index++
		includes = append(includes, path)
	}
	return includes, nil
}
