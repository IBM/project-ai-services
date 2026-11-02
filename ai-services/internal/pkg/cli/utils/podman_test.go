package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractPodConfigFromEnv(t *testing.T) {
	env := map[string]string{
		"AI_SERVICES_BASE_DIR":       "/var/lib/ai-services",
		"DOMAIN_SUFFIX":              "example.com",
		"CADDY_HTTPS_PORT":           "8443",
		"CADDY_PRIVATE_INGRESS_PORT": "9444",
		"GATEWAY_ADDR":               "gateway.example.com:9191",
		"WORKER_GATEWAY_PORT":        "9191",
	}

	var opts PodmanOptions
	ExtractPodConfigFromEnv(env, &opts)

	assert.Equal(t, "/var/lib/ai-services", opts.BaseDir)
	assert.Equal(t, "example.com", opts.DomainName)
	assert.Equal(t, 8443, opts.HTTPSPort)
	assert.Equal(t, 9444, opts.PrivateIngressPort)
	assert.Equal(t, "gateway.example.com:9191", opts.GatewayAddr)
	assert.Equal(t, 9191, opts.WorkerGatewayPort)
}
