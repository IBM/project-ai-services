package openshift

import (
	"context"
	"testing"

	semver "github.com/blang/semver/v4"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
			input:    "cert-manager.v1.3.1",
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
				currentChannel:   "stable-v1",
				currentCSV:       "cert-manager.v1.10.0",
				availableChannel: "stable-v1",
				availableCSV:     "cert-manager.v1.12.0",
			},
			expected: kindUpgrade,
		},
		{
			name: "CSV downgrade",
			versions: upgradeVersions{
				currentChannel:   "stable-v1",
				currentCSV:       "cert-manager.v1.12.0",
				availableChannel: "stable-v1",
				availableCSV:     "cert-manager.v1.10.0",
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

func TestReinstallSpyreOperator(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = operatorsv1alpha1.AddToScheme(scheme)

	t.Run("successfully deletes SCP, CSV, and Subscription", func(t *testing.T) {
		scp := &unstructured.Unstructured{}
		scp.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   constants.SpyreGroup,
			Version: constants.SpyreVersion,
			Kind:    constants.SpyreKind,
		})
		scp.SetName(constants.SpyreClusterPolicyName)

		sub := &operatorsv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.SpyreOperatorPackage,
				Namespace: constants.SpyreOperatorPackage,
			},
			Spec: &operatorsv1alpha1.SubscriptionSpec{
				Package: constants.SpyreOperatorPackage,
				Channel: "stable-v1.3",
			},
			Status: operatorsv1alpha1.SubscriptionStatus{
				InstalledCSV: "spyre-operator.v1.3.1",
			},
		}

		csv := &operatorsv1alpha1.ClusterServiceVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "spyre-operator.v1.3.1",
				Namespace: constants.SpyreOperatorPackage,
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(sub, csv, scp).
			Build()

		client := &openshift.OpenshiftClient{
			Client: fakeClient,
		}

		ctx := context.Background()
		s := spinner.New("Testing reinstall")

		err := reinstallSpyreOperator(ctx, client, sub, s)
		if err != nil {
			t.Fatalf("reinstallSpyreOperator failed: %v", err)
		}

		// Verify SpyreClusterPolicy is deleted
		checkSCP := &unstructured.Unstructured{}
		checkSCP.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   constants.SpyreGroup,
			Version: constants.SpyreVersion,
			Kind:    constants.SpyreKind,
		})
		if err := fakeClient.Get(ctx, clientpkgObjectKey(constants.SpyreClusterPolicyName, ""), checkSCP); !apierrors.IsNotFound(err) {
			t.Errorf("expected SpyreClusterPolicy to be deleted, got err: %v", err)
		}

		// Verify Subscription is deleted
		checkSub := &operatorsv1alpha1.Subscription{}
		if err := fakeClient.Get(ctx, clientpkgObjectKey("spyre-operator", "spyre-operator"), checkSub); !apierrors.IsNotFound(err) {
			t.Errorf("expected Subscription to be deleted, got err: %v", err)
		}

		// Verify CSV is deleted
		checkCSV := &operatorsv1alpha1.ClusterServiceVersion{}
		if err := fakeClient.Get(ctx, clientpkgObjectKey("spyre-operator.v1.3.1", "spyre-operator"), checkCSV); !apierrors.IsNotFound(err) {
			t.Errorf("expected ClusterServiceVersion to be deleted, got err: %v", err)
		}
	})

	t.Run("succeeds when SCP or CSV already absent", func(t *testing.T) {
		sub := &operatorsv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.SpyreOperatorPackage,
				Namespace: constants.SpyreOperatorPackage,
			},
			Spec: &operatorsv1alpha1.SubscriptionSpec{
				Package: constants.SpyreOperatorPackage,
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(sub).
			Build()

		client := &openshift.OpenshiftClient{
			Client: fakeClient,
		}

		ctx := context.Background()
		s := spinner.New("Testing reinstall")

		err := reinstallSpyreOperator(ctx, client, sub, s)
		if err != nil {
			t.Fatalf("reinstallSpyreOperator failed when resources absent: %v", err)
		}

		// Verify Subscription is deleted
		checkSub := &operatorsv1alpha1.Subscription{}
		if err := fakeClient.Get(ctx, clientpkgObjectKey("spyre-operator", "spyre-operator"), checkSub); !apierrors.IsNotFound(err) {
			t.Errorf("expected Subscription to be deleted, got err: %v", err)
		}
	})
}

func clientpkgObjectKey(name, namespace string) client.ObjectKey {
	return client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}
}
