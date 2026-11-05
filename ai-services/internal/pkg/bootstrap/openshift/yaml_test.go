package openshift

import (
	"testing"

	semver "github.com/blang/semver/v4"
)

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected semver.Version
		ok       bool
	}{
		{
			name:     "standard CSV",
			input:    "rhods-operator.3.5.0",
			expected: semver.MustParse("3.5.0"),
			ok:       true,
		},
		{
			name:     "CSV with v prefix",
			input:    "spyre-operator.v1.3.1",
			expected: semver.MustParse("1.3.1"),
			ok:       true,
		},
		{
			name:     "channel with major.minor",
			input:    "stable-3.5",
			expected: semver.MustParse("3.5.0"),
			ok:       true,
		},
		{
			name:     "channel with v prefix",
			input:    "stable-v1.3",
			expected: semver.MustParse("1.3.0"),
			ok:       true,
		},
		{
			name:     "channel with major only",
			input:    "stable-v1",
			expected: semver.MustParse("1.0.0"),
			ok:       true,
		},
		{
			name:     "channel with fast prefix",
			input:    "fast-4.14",
			expected: semver.MustParse("4.14.0"),
			ok:       true,
		},
		{
			name:     "unversioned channel",
			input:    "stable",
			expected: semver.Version{},
			ok:       false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: semver.Version{},
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := extractVersion(tt.input)
			if ok != tt.ok {
				t.Fatalf("extractVersion(%q) ok = %v, expected %v", tt.input, ok, tt.ok)
			}
			if ok && !v.Equals(tt.expected) {
				t.Fatalf("extractVersion(%q) = %v, expected %v", tt.input, v, tt.expected)
			}
		})
	}
}

func TestIsDowngrade(t *testing.T) {
	tests := []struct {
		name     string
		versions upgradeVersions
		expected bool
	}{
		{
			name: "CSV downgrade",
			versions: upgradeVersions{
				currentChannel:   "stable-v1.3",
				currentCSV:       "spyre-operator.v1.3.2",
				availableChannel: "stable-v1.3",
				availableCSV:     "spyre-operator.v1.3.0",
			},
			expected: true,
		},
		{
			name: "CSV upgrade",
			versions: upgradeVersions{
				currentChannel:   "stable-v1.3",
				currentCSV:       "spyre-operator.v1.3.0",
				availableChannel: "stable-v1.3",
				availableCSV:     "spyre-operator.v1.3.2",
			},
			expected: false,
		},
		{
			name: "Channel downgrade (no CSV)",
			versions: upgradeVersions{
				currentChannel:   "stable-3.6",
				currentCSV:       "rhods-operator.3.6.0",
				availableChannel: "stable-3.5",
				availableCSV:     "",
			},
			expected: true,
		},
		{
			name: "Channel downgrade (older channel than current CSV)",
			versions: upgradeVersions{
				currentChannel:   "fast-3.6",
				currentCSV:       "rhods-operator.3.6.1",
				availableChannel: "stable-3.5",
				availableCSV:     "",
			},
			expected: true,
		},
		{
			name: "Same channel patch (current CSV ahead of channel base)",
			versions: upgradeVersions{
				currentChannel:   "stable-3.5",
				currentCSV:       "rhods-operator.3.5.2",
				availableChannel: "stable-3.5",
				availableCSV:     "",
			},
			expected: false,
		},
		{
			name: "Lateral channel switch with same version",
			versions: upgradeVersions{
				currentChannel:   "fast-3.5",
				currentCSV:       "rhods-operator.3.5.0",
				availableChannel: "stable-3.5",
				availableCSV:     "rhods-operator.3.5.0",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDowngrade(tt.versions)
			if result != tt.expected {
				t.Fatalf("isDowngrade(%+v) = %v, expected %v", tt.versions, result, tt.expected)
			}
		})
	}
}

func TestIsUpgrade(t *testing.T) {
	tests := []struct {
		name     string
		versions upgradeVersions
		expected bool
	}{
		{
			name: "CSV upgrade",
			versions: upgradeVersions{
				currentChannel:   "stable-v1.3",
				currentCSV:       "spyre-operator.v1.3.0",
				availableChannel: "stable-v1.3",
				availableCSV:     "spyre-operator.v1.3.2",
			},
			expected: true,
		},
		{
			name: "Channel upgrade without CSV",
			versions: upgradeVersions{
				currentChannel:   "stable-3.5",
				currentCSV:       "rhods-operator.3.5.0",
				availableChannel: "stable-3.6",
				availableCSV:     "",
			},
			expected: true,
		},
		{
			name: "Lateral channel switch is not upgrade",
			versions: upgradeVersions{
				currentChannel:   "fast-3.5",
				currentCSV:       "rhods-operator.3.5.0",
				availableChannel: "stable-3.5",
				availableCSV:     "rhods-operator.3.5.0",
			},
			expected: false,
		},
		{
			name: "Unversioned channel switch is not upgrade",
			versions: upgradeVersions{
				currentChannel:   "alpha",
				currentCSV:       "operator.v1.0.0",
				availableChannel: "stable",
				availableCSV:     "operator.v1.0.0",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isUpgrade(tt.versions)
			if result != tt.expected {
				t.Fatalf("isUpgrade(%+v) = %v, expected %v", tt.versions, result, tt.expected)
			}
		})
	}
}
