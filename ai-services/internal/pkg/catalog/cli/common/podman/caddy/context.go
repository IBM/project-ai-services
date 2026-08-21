// Package caddy provides Caddy-specific operations for catalog deployment.
// This package handles Caddy proxy operations WITHOUT any template dependencies.
package caddy

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	certutils "github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// Context holds Caddy-specific configuration and cached values during catalog deployment.
// NOTE: This context does NOT handle templates - that's the responsibility of deployContext.
type Context struct {
	// Pod identification
	podName string
	// Network configuration
	domainSuffix string

	// Admin API access
	containerAdminURL string // http://<podName>:2019 (for container use in templates)
	hostAdminURL      string // http://localhost:<port> (for host VM use in post-deployment)

	sslCertContent   string
	sslKeyContent    string
	caddyFileContent string
}

// NewContext creates a new Caddy context with the pod name provided by configure.go.
// The pod name should be obtained from deployContext, not looked up here.
func NewContext(podName string, domainSuffix string) *Context {
	return &Context{
		podName:      podName,
		domainSuffix: domainSuffix,
	}
}

// GetHostAdminURL retrieves the Caddy admin URL for host VM use, caching the result.
func (c *Context) GetHostAdminURL() (string, error) {
	if c.hostAdminURL != "" {
		return c.hostAdminURL, nil
	}

	rt, err := podman.NewPodmanClient()
	if err != nil {
		return "", fmt.Errorf("failed to initialize podman client: %w", err)
	}

	adminPort, err := getCaddyAdminPort(rt, c.podName)
	if err != nil {
		return "", fmt.Errorf("failed to get Caddy admin port: %w", err)
	}

	c.hostAdminURL = fmt.Sprintf("http://localhost:%s", adminPort)

	return c.hostAdminURL, nil
}

// GetContainerAdminURL returns the Caddy admin URL for container use.
func (c *Context) GetContainerAdminURL() string {
	if c.containerAdminURL != "" {
		return c.containerAdminURL
	}

	c.containerAdminURL = fmt.Sprintf("http://%s:2019", c.podName)

	return c.containerAdminURL
}

// GetHTTPSPort retrieves the Caddy HTTPS port.
func (c *Context) GetHTTPSPort(rt *podman.PodmanClient) (string, error) {
	return getHTTPSPort(rt, c.podName)
}

// CreateProxyManager creates a Caddy proxy manager.
func (c *Context) CreateProxyManager() (proxy.ProxyManager, error) {
	adminURL, err := c.GetHostAdminURL()
	if err != nil {
		return nil, err
	}

	return proxy.NewCaddyManager(adminURL, constants.CaddyServerName), nil
}

// GetDomainSuffix returns the domain suffix.
func (c *Context) GetDomainSuffix() string {
	return c.domainSuffix
}

func (c *Context) LoadCertAndKeyContent(sslCertPath, sslKeyPath string) error {
	if sslCertPath == "" || sslKeyPath == "" {
		return nil
	}

	certbyte, keyBytes, _, err := certutils.ReadAndParseCertificates(sslCertPath, sslKeyPath)
	if err != nil {
		return err
	}
	c.sslCertContent = string(certbyte)
	c.sslKeyContent = string(keyBytes)

	return nil
}

func (c *Context) LoadCaddyFileContent() error {
	var err error
	if c.caddyFileContent, err = GetCaddyFileContent(); err != nil {
		return fmt.Errorf("failed to set caddy file content: %w", err)
	}

	return nil
}

func (c *Context) GetCaddyFileContent() string {
	return c.caddyFileContent
}

func (c *Context) GetSSLCertContent() string {
	return c.sslCertContent
}

func (c *Context) GetSSLKeyContent() string {
	return c.sslKeyContent
}

// Made with Bob
