package bindconfig

import (
	"errors"
	"strings"
)

const (
	zonesMarkerBegin = "// BEGIN CELIKPANEL MANAGED BIND ZONES"
	zonesMarkerEnd   = "// END CELIKPANEL MANAGED BIND ZONES"
)

// ErrManagedZoneIncludeModified identifies an owner edit inside the panel's
// managed span. Callers must preserve the file and stop automatic mutation.
var ErrManagedZoneIncludeModified = errors.New("existing CelikPanel BIND zone include was modified")

// VerifyExactZoneInclude accepts only a configuration already carrying the
// producer's exact active managed include; it never prepares or writes one.
func VerifyExactZoneInclude(config, includePath string) error {
	prepared, err := ManagedZoneInclude(config, includePath)
	if err != nil {
		return err
	}
	if prepared != config {
		return errors.New("managed BIND zone include is absent")
	}
	return nil
}
func ManagedZoneInclude(config, includePath string) (string, error) {
	if includePath == "" || !strings.HasPrefix(includePath, "/") ||
		strings.ContainsAny(includePath, "\x00\n\"\\") {
		return "", errors.New("invalid managed BIND zone include path")
	}
	block := zonesMarkerBegin + "\ninclude \"" + includePath + "\";\n" +
		zonesMarkerEnd + "\n"
	beginCount := strings.Count(config, zonesMarkerBegin)
	endCount := strings.Count(config, zonesMarkerEnd)
	if beginCount != endCount || beginCount > 1 {
		return "", errors.New("BIND zone include markers are incomplete or duplicated")
	}
	if beginCount == 1 {
		start := strings.Index(config, zonesMarkerBegin)
		endStart := strings.Index(config[start:], zonesMarkerEnd)
		if endStart < 0 {
			return "", errors.New("BIND zone include marker is incomplete")
		}
		endStart += start
		if !MarkerStartsActiveComment(config, start) ||
			!MarkerStartsActiveComment(config, endStart) {
			return "", errors.New("BIND zone include markers are not active configuration comments")
		}
		end := endStart + len(zonesMarkerEnd)
		if end < len(config) && config[end] == '\r' {
			end++
		}
		if end < len(config) && config[end] == '\n' {
			end++
		}
		if config[start:end] != block {
			return "", ErrManagedZoneIncludeModified
		}
		return config, nil
	}
	if strings.Contains(config, includePath) {
		return "", errors.New("managed BIND zone include exists outside its ownership markers")
	}
	if config != "" && !strings.HasSuffix(config, "\n") {
		config += "\n"
	}
	if config != "" {
		config += "\n"
	}
	return config + block, nil
}

func MarkerStartsActiveComment(config string, markerStart int) bool {
	if markerStart < 0 || markerStart+1 >= len(config) ||
		config[markerStart] != '/' || config[markerStart+1] != '/' {
		return false
	}
	const (
		bindLexCode = iota
		bindLexString
		bindLexLineComment
		bindLexBlockComment
	)
	state := bindLexCode
	for index := 0; index < markerStart; {
		switch state {
		case bindLexCode:
			switch {
			case config[index] == '"':
				state = bindLexString
				index++
			case config[index] == '#':
				state = bindLexLineComment
				index++
			case config[index] == '/' && index+1 < markerStart && config[index+1] == '/':
				state = bindLexLineComment
				index += 2
			case config[index] == '/' && index+1 < markerStart && config[index+1] == '*':
				state = bindLexBlockComment
				index += 2
			default:
				index++
			}
		case bindLexString:
			if config[index] == '\\' && index+1 < markerStart {
				index += 2
				continue
			}
			if config[index] == '"' {
				state = bindLexCode
			}
			index++
		case bindLexLineComment:
			if config[index] == '\n' {
				state = bindLexCode
			}
			index++
		case bindLexBlockComment:
			if config[index] == '*' && index+1 < markerStart && config[index+1] == '/' {
				state = bindLexCode
				index += 2
				continue
			}
			index++
		}
	}
	return state == bindLexCode
}
