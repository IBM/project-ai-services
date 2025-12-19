package utils

import (
	"fmt"
	"maps"
	"net"
	"os"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"go.yaml.in/yaml/v3"
)

// BoolPtr -> converts to bool ptr
func BoolPtr(v bool) *bool {
	return &v
}

// flattenArray takes a 2D slice and returns a 1D slice with all values
func FlattenArray[T comparable](arr [][]T) []T {
	flatArr := []T{}

	for _, row := range arr {
		flatArr = append(flatArr, row...)
	}

	return flatArr
}

// ExtractMapKeys returns a slice of map keys
func ExtractMapKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

// CopyMap - does a shallow copy of input map
// Note -> this does a shallow copy works for only primitive types
func CopyMap[K comparable, V any](src map[K]V) map[K]V {
	dst := make(map[K]V, len(src))
	maps.Copy(dst, src)

	return dst
}

// JoinAndRemove joins the first `count` elements using `sep`,
// returns the joined string, and removes those elements from the original slice.
func JoinAndRemove(slice *[]string, count int, sep string) string {
	if len(*slice) == 0 {
		return ""
	}
	if count > len(*slice) {
		count = len(*slice)
	}

	joinedStr := strings.Join((*slice)[:count], sep)
	*slice = (*slice)[count:] // modify the original slice

	return joinedStr
}

func UniqueSlice[T comparable](slice []T) []T {
	seen := make(map[T]bool)
	var result []T

	for _, item := range slice {
		if _, ok := seen[item]; !ok {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

func ParseKeyValues(pairs []string) (map[string]string, error) {
	out := map[string]string{}

	for _, pair := range pairs {
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid format: %s (expected key=value)", pair)
		}
		out[kv[0]] = kv[1]
	}

	return out, nil
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	logger.Errorf("Error checking file existence: %v\n", err)

	return false
}

func GetHostIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", nil
}

// Checks if a yaml.Node is marked as hidden via @hidden in the head comment
func isHidden(n *yaml.Node) bool {
	if n == nil {
		return false
	}

	return strings.Contains(n.HeadComment, "@hidden")
}

// Retrieves the description from a yaml.Node's head comment marked with @description
func getDescription(n *yaml.Node) string {
	if n == nil {
		return ""
	}

	comment := n.HeadComment
	idx := strings.Index(comment, "@description")
	if idx < 0 {
		return ""
	}

	desc := comment[idx+len("@description"):]

	return strings.TrimSpace(desc)
}

func FlattenNode(prefix string, n *yaml.Node, descMap map[string]string) {
	if n == nil {
		return
	}

	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			keyNode := n.Content[i]
			valNode := n.Content[i+1]

			if isHidden(keyNode) {
				continue
			}

			var newPrefix string
			if prefix == "" {
				newPrefix = keyNode.Value
			} else {
				newPrefix = prefix + "." + keyNode.Value
			}

			// description tied to key
			if d := getDescription(keyNode); d != "" {
				descMap[newPrefix] = d
			}

			FlattenNode(newPrefix, valNode, descMap)
		}

	case yaml.SequenceNode:
		for i, el := range n.Content {
			newPrefix := fmt.Sprintf("%s[%d]", prefix, i)

			// sequences probably not commented, but if they are:
			if d := getDescription(el); d != "" {
				descMap[newPrefix] = d
			}

			FlattenNode(newPrefix, el, descMap)
		}

	default:
		// Leaf values
		if prefix != "" {
			if d := getDescription(n); d != "" {
				descMap[prefix] = d
			}
		}
	}
}

// This function sets a nested value in a map based on a dotted key notation.
// For example, converts ui.port = value to map["ui"]["port"] = value
// It modifies the input map in place, no return value.
func SetNestedValue(out map[string]any, dottedKey string, value any) {
	//dottedKey of the form ui.image, ui.port, etc.
	parts := strings.Split(dottedKey, ".")
	current := out

	for i := 0; i < len(parts)-1; i++ {
		key := parts[i]

		if next, ok := current[key]; ok {
			if cast, ok := next.(map[string]any); ok {
				current = cast
			} else {
				newMap := map[string]any{}
				current[key] = newMap
				current = newMap
			}
		} else {
			newMap := map[string]any{}
			current[key] = newMap
			current = newMap
		}
	}
	last := parts[len(parts)-1]
	current[last] = value
}

func VerifyAppName(appName string) error {
	if appName == "" || strings.Contains(appName, "..") || strings.ContainsAny(appName, "/\\") {
		return fmt.Errorf("invalid application name: %s", appName)
	}

	return nil
}

func ValidateParams(params map[string]string, supportedParams map[string]any) error {
	for param := range params {
		key := param
		if strings.Contains(param, "=") {
			key = strings.Split(param, "=")[0]
		}

		if !checkParamsInValues(key, supportedParams) {
			return fmt.Errorf("unsupported parameter: %s", key)
		}
	}

	return nil
}

/*
checkParamsInValues traverses the nested map structure, and return true only if the full path exists.
Eg: for param = "ui.port", it checks if values["ui"]["port"] exists.
*/
func checkParamsInValues(param string, values map[string]any) bool {
	// Considering example: "ui.port" which becomes ["ui", "port"] after below step
	parts := strings.Split(param, ".")
	current := values

	for i, key := range parts {
		// Check if the current key exists in the current map level
		val, ok := current[key]
		if !ok {
			// Key doesn't exists at this level, so parameter path is invalid
			// Example: if "ui" doesn't exist in values, return false
			return false
		}
		// If we have reached the last part of the path, the parameter exists
		// Example: for "ui.port", when i=1 (on "port"), we found it; hence returning true
		if i == len(parts)-1 {
			return true
		}
		// If it is not the last part, we need to go deeper into the nested structure,
		// so we try to cast the value to a map so we can continue.
		// Example: for "ui.port", when i=0 (on "ui"), we need values["ui"] to be a map
		cast, ok := val.(map[string]any)
		if !ok {
			// Value exists but isn't a map, so we cant traverse further
			// Example: if user supplies "ui.port.number" but port is a string, so return false
			return false
		}

		// Move the pointer deeper for the next iteration
		current = cast
	}

	return false
}
