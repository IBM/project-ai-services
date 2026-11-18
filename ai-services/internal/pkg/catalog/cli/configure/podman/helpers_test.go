package podman

import (
	"testing"

	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/stretchr/testify/assert"
)

func TestValidateRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		opts    *catalogUtils.PodmanConfigureOptions
		wantErr string
	}{
		{
			name: "all valid",
			opts: &catalogUtils.PodmanConfigureOptions{
				DomainName:         "example.com",
				HttpsPort:          443,
				PrivateIngressPort: 9443,
				BaseDir:            "/var/lib/ai-services",
			},
			wantErr: "",
		},
		{
			name: "missing domain",
			opts: &catalogUtils.PodmanConfigureOptions{
				HttpsPort:          443,
				PrivateIngressPort: 9443,
				BaseDir:            "/var/lib/ai-services",
			},
			wantErr: "DOMAIN_SUFFIX environment variable not found in catalog pod",
		},
		{
			name: "missing https port",
			opts: &catalogUtils.PodmanConfigureOptions{
				DomainName:         "example.com",
				PrivateIngressPort: 9443,
				BaseDir:            "/var/lib/ai-services",
			},
			wantErr: "CADDY_HTTPS_PORT environment variable not found in catalog pod",
		},
		{
			name: "missing private ingress port",
			opts: &catalogUtils.PodmanConfigureOptions{
				DomainName: "example.com",
				HttpsPort:  443,
				BaseDir:    "/var/lib/ai-services",
			},
			wantErr: "CADDY_PRIVATE_INGRESS_PORT environment variable not found in catalog pod",
		},
		{
			name: "missing base dir",
			opts: &catalogUtils.PodmanConfigureOptions{
				DomainName:         "example.com",
				HttpsPort:          443,
				PrivateIngressPort: 9443,
			},
			wantErr: "AI_SERVICES_BASE_DIR environment variable not found in catalog pod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequiredFields(tt.opts)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestValidateConfigParameters(t *testing.T) {
	baseExisting := catalogUtils.PodmanConfigureOptions{
		DomainName:         "example.com",
		HttpsPort:          443,
		PrivateIngressPort: 9443,
		BaseDir:            "/var/lib/ai-services",
	}

	tests := []struct {
		name         string
		existingOpts catalogUtils.PodmanConfigureOptions
		newOpts      catalogUtils.PodmanConfigureOptions
		domainSuffix string
		wantErr      string
	}{
		{
			name:         "identical parameters succeed",
			existingOpts: baseExisting,
			newOpts:      baseExisting,
			domainSuffix: "example.com",
			wantErr:      "",
		},
		{
			name:         "domain changed",
			existingOpts: baseExisting,
			newOpts:      baseExisting,
			domainSuffix: "other.com",
			wantErr:      "domain change not allowed during reconfigure: existing=example.com, new=other.com. Please uninstall the catalog deployment and re-run configure to change domain",
		},
		{
			name:         "https port changed",
			existingOpts: baseExisting,
			newOpts: catalogUtils.PodmanConfigureOptions{
				DomainName:         "example.com",
				HttpsPort:          8443,
				PrivateIngressPort: 9443,
				BaseDir:            "/var/lib/ai-services",
			},
			domainSuffix: "example.com",
			wantErr:      "HTTPS port change not allowed during reconfigure: existing=443, new=8443. Please uninstall the catalog deployment and re-run configure to change https port",
		},
		{
			name:         "private ingress port changed",
			existingOpts: baseExisting,
			newOpts: catalogUtils.PodmanConfigureOptions{
				DomainName:         "example.com",
				HttpsPort:          443,
				PrivateIngressPort: 9444,
				BaseDir:            "/var/lib/ai-services",
			},
			domainSuffix: "example.com",
			wantErr:      "private ingress port change not allowed during reconfigure: existing=9443, new=9444. Please uninstall the catalog deployment and re-run configure to change private ingress port",
		},
		{
			name:         "base dir changed",
			existingOpts: baseExisting,
			newOpts: catalogUtils.PodmanConfigureOptions{
				DomainName:         "example.com",
				HttpsPort:          443,
				PrivateIngressPort: 9443,
				BaseDir:            "/opt/ai-services",
			},
			domainSuffix: "example.com",
			wantErr:      "base directory change not allowed during reconfigure: existing=/var/lib/ai-services, new=/opt/ai-services. Please uninstall the catalog deployment and re-run configure to change base directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfigParameters(&tt.existingOpts, &tt.newOpts, tt.domainSuffix)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}
