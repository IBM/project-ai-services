package utils

import (
    "encoding/json"
    "io"
)

func DecodeJSON[T any](reader io.Reader, v *T) error {
    decoder := json.NewDecoder(reader)
    return decoder.Decode(v)
}
