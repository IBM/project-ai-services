package utils

import (
	"encoding/json"
	"io"
)

// DecodeJSON decodes JSON from an io.Reader into a generic type.
func DecodeJSON[T any](reader io.Reader, v *T) error {
	decoder := json.NewDecoder(reader)

	return decoder.Decode(v)
}
