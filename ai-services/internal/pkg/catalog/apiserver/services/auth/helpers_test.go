package auth_test

import "encoding/json"

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)

	return string(b), err
}
