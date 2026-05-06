package utils

import (
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"go.yaml.in/yaml/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	maxKeyValueParts  = 2
	maxHostnameLength = 63
)

// IsTransientK8sError checks if a Kubernetes API error is transient and should be retried.
func IsTransientK8sError(err error) bool {
	return apierrors.IsTooManyRequests(err) || apierrors.IsServerTimeout(err) || apierrors.IsTimeout(err)
}

// BoolPtr -> converts to bool ptr.
func BoolPtr(v bool) *bool {
	return &v
}

// FlattenArray takes a 2D slice and returns a 1D slice with all values.
func FlattenArray[T comparable](arr [][]T) []T {
	flatArr := []T{}

	for _, row := range arr {
		flatArr = append(flatArr, row...)
	}

	return flatArr
}

// ExtractMapKeys returns a slice of map keys.
func ExtractMapKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

// CopyMap - does a shallow copy of input map.
// Note -> this does a shallow copy works for only primitive types.
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
		kv := strings.SplitN(pair, "=", maxKeyValueParts)
		if len(kv) != maxKeyValueParts {
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

// Checks if a yaml.Node is marked as hidden via @hidden in the head comment.
func isHidden(n *yaml.Node) bool {
	if n == nil {
		return false
	}

	return strings.Contains(n.HeadComment, "@hidden")
}

// Retrieves the description from a yaml.Node's head comment marked with @description.
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
		flattenMapping(prefix, n, descMap)
	case yaml.SequenceNode:
		flattenSequence(prefix, n, descMap)
	default:
		storeDescription(prefix, n, descMap)
	}
}

func flattenMapping(prefix string, n *yaml.Node, descMap map[string]string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode := n.Content[i]
		valNode := n.Content[i+1]

		if isHidden(keyNode) {
			continue
		}

		newPrefix := joinPrefix(prefix, keyNode.Value)
		storeDescription(newPrefix, keyNode, descMap)

		FlattenNode(newPrefix, valNode, descMap)
	}
}

func flattenSequence(prefix string, n *yaml.Node, descMap map[string]string) {
	for i, el := range n.Content {
		newPrefix := fmt.Sprintf("%s[%d]", prefix, i)
		storeDescription(newPrefix, el, descMap)

		FlattenNode(newPrefix, el, descMap)
	}
}

func storeDescription(prefix string, n *yaml.Node, descMap map[string]string) {
	if prefix == "" {
		return
	}
	if d := getDescription(n); d != "" {
		descMap[prefix] = d
	}
}

func joinPrefix(prefix, key string) string {
	if prefix == "" {
		return key
	}

	return prefix + "." + key
}

// SetNestedValue function sets a nested value in a map based on a dotted key notation.
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

// rfc1035HostnameRegex validates RFC 1035 hostname format:
// - 1-63 characters.
// - lowercase letters, numbers, and hyphens only.
// - must start with a letter.
// - must end with a letter or number.
var rfc1035HostnameRegex = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

func VerifyAppName(appName string) error {
	if appName == "" {
		return fmt.Errorf("application name cannot be empty")
	}

	if len(appName) > maxHostnameLength {
		return fmt.Errorf("invalid application name '%s': must be %d characters or less", appName, maxHostnameLength)
	}

	if !rfc1035HostnameRegex.MatchString(appName) {
		return fmt.Errorf("invalid application name '%s': must be a valid RFC 1035 hostname (lowercase letters, numbers, and hyphens only; must start with a letter and end with a letter or number)", appName)
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

// GetExistingCustomResource checks if a single instance resource exists and return the object.
func GetExistingCustomResource(client *openshift.OpenshiftClient, gvk schema.GroupVersionKind) (*unstructured.Unstructured, bool, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	if err := client.Client.List(client.Ctx, list); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		// Handle rate limiting and other transient errors as retryable by returning them
		// The caller's polling loop will retry
		if IsTransientK8sError(err) {
			return nil, false, err
		}

		return nil, false, fmt.Errorf("error listing %s: %w", gvk.Kind, err)
	}

	if len(list.Items) == 0 {
		return nil, false, nil
	}

	return &list.Items[0], true, nil
}

// FlattenMapToKeys converts a nested map into a flat map with dotted keys
// Example: {"ui": {"port": "8080"}} -> {"ui.port": ""}.
func FlattenMapToKeys(m map[string]any, prefix string) map[string]string {
	result := make(map[string]string)
	for key, val := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		// If the value is a nested map, recurse
		if nestedMap, ok := val.(map[string]any); ok {
			nestedKeys := FlattenMapToKeys(nestedMap, fullKey)
			maps.Copy(result, nestedKeys)
		} else {
			// For leaf values, add the key
			result[fullKey] = ""
		}
	}

	return result
}

// GetEnv retrieves an environment variable or returns a default value if not set.
func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return defaultValue
}

// GetBaseDir returns the base directory from environment variable or default.
func GetBaseDir() string {
	baseDir := constants.DefaultBaseDir
	if dir := os.Getenv("AI_SERVICES_BASE_DIR"); dir != "" {
		baseDir = dir
	}

	return baseDir
}

// GetApplicationsPath returns the applications path based on the configured base directory.
func GetApplicationsPath() string {
	return filepath.Join(GetBaseDir(), "applications")
}

// GetModelsPath returns the models path based on the configured base directory.
func GetModelsPath() string {
	return filepath.Join(GetBaseDir(), "models")
}

// ValidateBaseDir validates that the base directory exists or can be created.
// It always appends 'ai-services' subdirectory to the provided base directory for all AI services content.
func ValidateBaseDir(baseDir string) (string, error) {
	// Clean the path and append ai-services subdirectory
	baseDir = filepath.Join(filepath.Clean(baseDir), "ai-services")

	// Check if directory exists or can be created
	if err := os.MkdirAll(baseDir, constants.DirPerm); err != nil {
		return "", fmt.Errorf("cannot create directory: %w", err)
	}

	return baseDir, nil
}
