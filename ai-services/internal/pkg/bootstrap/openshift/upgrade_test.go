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
			name:     "package name containing digits",
			input:    "servicemeshoperator3.v3.1.0",
			expected: semver.MustParse("3.1.0"),
			ok:       true,
		},
		{
			name:     "package name with digit and hyphen",
			input:    "openshift-gitops-1.v1.10.0",
			expected: semver.MustParse("1.10.0"),
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

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name     string
		versions upgradeVersions
		expected upgradeKind
	}{
		{
			name: "Same version and channel",
			versions: upgradeVersions{
				currentChannel:   "stable-3.5",
				currentCSV:       "rhods-operator.3.5.0",
				availableChannel: "stable-3.5",
				availableCSV:     "rhods-operator.3.5.0",
			},
			expected: kindSame,
		},
		{
			name: "Same channel without available CSV",
			versions: upgradeVersions{
				currentChannel:   "stable-3.5",
				currentCSV:       "rhods-operator.3.5.2",
				availableChannel: "stable-3.5",
				availableCSV:     "",
			},
			expected: kindSame,
		},
		{
			name: "CSV upgrade",
			versions: upgradeVersions{
				currentChannel:   "stable-v1.3",
				currentCSV:       "spyre-operator.v1.3.0",
				availableChannel: "stable-v1.3",
				availableCSV:     "spyre-operator.v1.3.2",
			},
			expected: kindUpgrade,
		},
		{
			name: "CSV downgrade",
			versions: upgradeVersions{
				currentChannel:   "stable-v1.3",
				currentCSV:       "spyre-operator.v1.3.2",
				availableChannel: "stable-v1.3",
				availableCSV:     "spyre-operator.v1.3.0",
			},
			expected: kindDowngrade,
		},
		{
			name: "Channel upgrade without CSV",
			versions: upgradeVersions{
				currentChannel:   "stable-3.5",
				currentCSV:       "rhods-operator.3.5.0",
				availableChannel: "stable-3.6",
				availableCSV:     "",
			},
			expected: kindUpgrade,
		},
		{
			name: "Channel downgrade (no CSV)",
			versions: upgradeVersions{
				currentChannel:   "stable-3.6",
				currentCSV:       "rhods-operator.3.6.0",
				availableChannel: "stable-3.5",
				availableCSV:     "",
			},
			expected: kindDowngrade,
		},
		{
			name: "Channel downgrade (older channel than current CSV)",
			versions: upgradeVersions{
				currentChannel:   "fast-3.6",
				currentCSV:       "rhods-operator.3.6.1",
				availableChannel: "stable-3.5",
				availableCSV:     "",
			},
			expected: kindDowngrade,
		},
		{
			name: "Lateral channel switch with same version",
			versions: upgradeVersions{
				currentChannel:   "fast-3.5",
				currentCSV:       "rhods-operator.3.5.0",
				availableChannel: "stable-3.5",
				availableCSV:     "rhods-operator.3.5.0",
			},
			expected: kindChannelSwitch,
		},
		{
			name: "Unversioned channel switch",
			versions: upgradeVersions{
				currentChannel:   "alpha",
				currentCSV:       "operator.v1.0.0",
				availableChannel: "stable",
				availableCSV:     "operator.v1.0.0",
			},
			expected: kindChannelSwitch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareVersions(tt.versions)
			if result != tt.expected {
				t.Fatalf("compareVersions(%+v) = %v, expected %v", tt.versions, result, tt.expected)
			}
		})
	}
}

func TestIsTargetCSVReached(t *testing.T) {
	tests := []struct {
		name         string
		installedCSV string
		targetCSV    string
		previousCSV  string
		expected     bool
	}{
		{
			name:         "empty installed CSV",
			installedCSV: "",
			targetCSV:    "rhods-operator.3.5.2",
			previousCSV:  "rhods-operator.3.5.0",
			expected:     false,
		},
		{
			name:         "upgrade with target CSV - still on previous",
			installedCSV: "rhods-operator.3.5.0",
			targetCSV:    "rhods-operator.3.5.2",
			previousCSV:  "rhods-operator.3.5.0",
			expected:     false,
		},
		{
			name:         "upgrade with target CSV - reached exact target",
			installedCSV: "rhods-operator.3.5.2",
			targetCSV:    "rhods-operator.3.5.2",
			previousCSV:  "rhods-operator.3.5.0",
			expected:     true,
		},
		{
			name:         "upgrade with target CSV - reached higher patch",
			installedCSV: "rhods-operator.3.5.3",
			targetCSV:    "rhods-operator.3.5.2",
			previousCSV:  "rhods-operator.3.5.0",
			expected:     true,
		},
		{
			name:         "upgrade without target CSV - still on previous",
			installedCSV: "cert-manager.v1.10.0",
			targetCSV:    "",
			previousCSV:  "cert-manager.v1.10.0",
			expected:     false,
		},
		{
			name:         "upgrade without target CSV - changed from previous",
			installedCSV: "cert-manager.v1.12.0",
			targetCSV:    "",
			previousCSV:  "cert-manager.v1.10.0",
			expected:     true,
		},
		{
			name:         "fresh install with target CSV - matches target",
			installedCSV: "rhods-operator.3.5.2",
			targetCSV:    "rhods-operator.3.5.2",
			previousCSV:  "",
			expected:     true,
		},
		{
			name:         "fresh install without target CSV - installed non-empty",
			installedCSV: "cert-manager.v1.12.0",
			targetCSV:    "",
			previousCSV:  "",
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTargetCSVReached(tt.installedCSV, tt.targetCSV, tt.previousCSV)
			if result != tt.expected {
				t.Fatalf("isTargetCSVReached(%q, %q, %q) = %v, expected %v",
					tt.installedCSV, tt.targetCSV, tt.previousCSV, result, tt.expected)
			}
		})
	}
}
