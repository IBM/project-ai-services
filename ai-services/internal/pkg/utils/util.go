package utils

import (
	"fmt"
	"maps"
	"net"
	"strings"
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
