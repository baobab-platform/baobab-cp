package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var errDuplicateJSON = errors.New("duplicate JSON object member")

// rejectDuplicateJSON walks exactly one JSON value and rejects duplicate object
// members at any depth. encoding/json otherwise accepts last-member-wins input,
// which is unsuitable for private authority and evidence publication payloads.
func rejectDuplicateJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON value")
	}
	return nil
}

func uniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}

	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid JSON object member")
			}
			if _, duplicate := seen[name]; duplicate {
				return errDuplicateJSON
			}
			seen[name] = struct{}{}
			if err := uniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}
