package manifestv2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

var rawMessageType = reflect.TypeOf(json.RawMessage{})

// strictJSON rejects duplicate keys, non-canonical field case, unknown fields,
// and trailing values before decoding signed JSON into a typed value.
// strictJSON, imzalı JSON'u türü belirlenmiş bir değere çözmeden önce yinelenen
// anahtarları, standart dışı alan harf biçimini, bilinmeyen alanları ve artçı
// değerleri reddeder.
func strictJSON(data []byte, target any) error {
	if err := validateJSONKeys(data); err != nil {
		return err
	}
	if err := validateExactJSONFields(json.RawMessage(data), reflect.TypeOf(target)); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

// strictJSONObjectExtension validates an extension object without assigning
// semantics to its keys while still rejecting ambiguous signed JSON.
// strictJSONObjectExtension, anahtarlarına anlam yüklemeden bir genişletme
// nesnesini doğrular ve belirsiz imzalı JSON'u yine de reddeder.
func strictJSONObjectExtension(data []byte) error {
	if err := validateJSONKeys(data); err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return fmt.Errorf("JSON value must be an object")
	}
	return nil
}

func validateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, "$"); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON data")
		}
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, location string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode JSON at %s: %w", location, err)
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode JSON object key at %s: %w", location, err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object at %s has a non-string key", location)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON key %q at %s", key, location)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, location+"."+key); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("decode JSON object end at %s", location)
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", location, index)); err != nil {
				return err
			}
			index++
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("decode JSON array end at %s", location)
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delimiter, location)
	}
	return nil
}

func validateExactJSONFields(raw json.RawMessage, targetType reflect.Type) error {
	for targetType.Kind() == reflect.Pointer {
		targetType = targetType.Elem()
	}
	if targetType == rawMessageType || string(raw) == "null" {
		return nil
	}
	switch targetType.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			return fmt.Errorf("JSON value for %s must be an object", targetType)
		}
		fields := map[string]reflect.Type{}
		for index := 0; index < targetType.NumField(); index++ {
			field := targetType.Field(index)
			if field.PkgPath != "" {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			fields[name] = field.Type
		}
		for name, value := range object {
			fieldType, ok := fields[name]
			if !ok {
				return fmt.Errorf("unknown or non-canonical JSON field %q for %s", name, targetType)
			}
			if err := validateExactJSONFields(value, fieldType); err != nil {
				return err
			}
		}
	case reflect.Map:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			return fmt.Errorf("JSON value for %s must be an object", targetType)
		}
		for _, value := range object {
			if err := validateExactJSONFields(value, targetType.Elem()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if targetType == rawMessageType {
			return nil
		}
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return fmt.Errorf("JSON value for %s must be an array", targetType)
		}
		for _, value := range values {
			if err := validateExactJSONFields(value, targetType.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
