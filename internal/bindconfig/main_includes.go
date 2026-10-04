package bindconfig

import (
	"errors"
	"fmt"
	"strings"
)

// VerifyMainIncludes checks the fixed APT main file's active top-level
// include statements. This lexical proof is not a full BIND grammar check.
func VerifyMainIncludes(config string) error {
	counts, err := topLevelIncludes(config)
	if err != nil {
		return err
	}
	for _, path := range []string{
		"/etc/bind/named.conf.options", "/etc/bind/named.conf.local",
	} {
		if counts[path] != 1 {
			return fmt.Errorf("APT BIND main config lacks one active top-level include of %s", path)
		}
	}
	return nil
}

func topLevelIncludes(config string) (map[string]int, error) {
	counts := make(map[string]int)
	depth := 0
	for index := 0; index < len(config); {
		if next, skipped, err := skipBINDTrivia(config, index); err != nil {
			return nil, err
		} else if skipped {
			index = next
			continue
		}
		switch config[index] {
		case '"':
			next, _, err := readBINDString(config, index)
			if err != nil {
				return nil, err
			}
			index = next
		case '{':
			depth++
			index++
		case '}':
			depth--
			if depth < 0 {
				return nil, errors.New("BIND main config has an unmatched closing brace")
			}
			index++
		default:
			if !bindMainIdentifierStart(config[index]) {
				index++
				continue
			}
			start := index
			for index < len(config) && bindMainIdentifierPart(config[index]) {
				index++
			}
			if depth != 0 || config[start:index] != "include" {
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
				return nil, errors.New("BIND main config has a non-canonical include path")
			}
			next, included, err := readBINDString(config, index)
			if err != nil || !strings.HasPrefix(included, "/") {
				return nil, errors.New("BIND main config has a non-canonical include path")
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
				return nil, errors.New("BIND main config has an incomplete include statement")
			}
			counts[included]++
			index++
		}
	}
	if depth != 0 {
		return nil, errors.New("BIND main config has unclosed braces")
	}
	return counts, nil
}

func skipBINDTrivia(config string, index int) (int, bool, error) {
	if index >= len(config) {
		return index, false, nil
	}
	switch {
	case strings.ContainsRune(" \t\r\n", rune(config[index])):
		return index + 1, true, nil
	case config[index] == '#', strings.HasPrefix(config[index:], "//"):
		for index < len(config) && config[index] != '\n' {
			index++
		}
		return index, true, nil
	case strings.HasPrefix(config[index:], "/*"):
		end := strings.Index(config[index+2:], "*/")
		if end < 0 {
			return 0, false, errors.New("BIND main config has an unclosed block comment")
		}
		return index + 2 + end + 2, true, nil
	default:
		return index, false, nil
	}
}

func readBINDString(config string, start int) (int, string, error) {
	for index := start + 1; index < len(config); index++ {
		if config[index] == '\\' {
			return 0, "", errors.New("BIND main config has an escaped string")
		}
		if config[index] == '\n' || config[index] == '\r' {
			return 0, "", errors.New("BIND main config has an unclosed string")
		}
		if config[index] == '"' {
			return index + 1, config[start+1 : index], nil
		}
	}
	return 0, "", errors.New("BIND main config has an unclosed string")
}

func bindMainIdentifierStart(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value == '_'
}

func bindMainIdentifierPart(value byte) bool {
	return bindMainIdentifierStart(value) || value >= '0' && value <= '9' || value == '-'
}
