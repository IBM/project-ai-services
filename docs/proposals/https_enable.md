# HTTPS Enablement Proposal

## Overview

This proposal outlines the approach for enabling HTTPS/SSL for service endpoints in the AI Services project. It evaluates different options for implementing secure communication and provides a recommendation based on project requirements.

## Why SSL/TLS is Required

### Security Requirements

1. **Data Encryption in Transit**
   - Protects sensitive data (API keys, user credentials, model inputs/outputs) from interception
   - Prevents man-in-the-middle (MITM) attacks
   - Essential for compliance with security standards (SOC 2, ISO 27001, GDPR)

2. **Authentication & Trust**
   - Validates server identity through certificate verification
   - Prevents impersonation attacks
   - Builds user confidence in the service

3. **Data Integrity**
   - Ensures data is not tampered with during transmission
   - Detects any modifications to requests/responses
   - Maintains consistency of AI model interactions

4. **Compliance & Best Practices**
   - Required for production deployments
   - Industry standard for web services and APIs
   - Necessary for integration with enterprise systems
   - Browser requirements for modern web features

5. **API Security**
   - Protects authentication tokens and API keys
   - Secures sensitive AI model queries and responses
   - Prevents credential theft and session hijacking

## Options for Enabling HTTPS

### Option 1: Caddy Server

**Description**: Modern, automatic HTTPS server with built-in certificate management.

**Advantages**:
- ✅ Automatic self-signed certificates for quick HTTPS enablement
- ✅ Support for user-provided certificates (custom CA or purchased certificates)
- ✅ Zero-configuration TLS for development
- ✅ Simple configuration with Caddyfile
- ✅ Built-in reverse proxy capabilities
- ✅ HTTP/2 and HTTP/3 support out of the box
- ✅ Minimal resource footprint
- ✅ Easy to containerize and deploy
- ✅ Excellent for both development and production

**Disadvantages**:
- ⚠️ Relatively newer compared to nginx (though mature and stable)
- ⚠️ Smaller ecosystem of plugins compared to nginx

**Configuration Example**:
```caddyfile
# Caddyfile
{
    auto_https off  # For development with self-signed certs
}

localhost:8443 {
    reverse_proxy catalog-service:8080
    tls internal  # Generates self-signed cert automatically
}

# Production with user-provided certificates
api.example.com {
    reverse_proxy catalog-service:8080
    tls /path/to/cert.pem /path/to/key.pem
}
```

**Use Cases**:
- Development environments with automatic self-signed certificates
- Production deployments with user-provided certificates
- Microservices requiring simple HTTPS termination

### Option 2: Nginx

**Description**: High-performance web server and reverse proxy with extensive SSL/TLS support.

**Advantages**:
- ✅ Battle-tested and widely adopted
- ✅ Excellent performance and scalability
- ✅ Rich ecosystem of modules and plugins
- ✅ Extensive documentation and community support
- ✅ Fine-grained control over SSL/TLS configuration
- ✅ Advanced load balancing capabilities

**Disadvantages**:
- ⚠️ Manual certificate management required
- ⚠️ More complex configuration syntax
- ⚠️ Requires separate tools for certificate automation (certbot)
- ⚠️ More configuration overhead for basic HTTPS
- ⚠️ Steeper learning curve

**Configuration Example**:
```nginx
server {
    listen 443 ssl http2;
    server_name api.example.com;

    ssl_certificate /etc/nginx/ssl/cert.pem;
    ssl_certificate_key /etc/nginx/ssl/key.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    location / {
        proxy_pass http://catalog-service:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

**Use Cases**:
- High-traffic production environments
- Complex routing and load balancing requirements
- Organizations with existing nginx expertise

### Option 3: OpenSSL with Application-Level TLS

**Description**: Implementing TLS directly in the application using OpenSSL libraries.

**Advantages**:
- ✅ No external dependencies or reverse proxy needed
- ✅ Direct control over TLS implementation
- ✅ Reduced network hops
- ✅ Fine-grained control over certificate handling

**Disadvantages**:
- ⚠️ Requires significant development effort
- ⚠️ Application code becomes responsible for security
- ⚠️ Manual certificate management and renewal
- ⚠️ Increased complexity in application codebase
- ⚠️ Harder to maintain and update TLS configurations
- ⚠️ Security vulnerabilities if not implemented correctly
- ⚠️ Difficult to separate concerns (business logic vs. security)

**Implementation Considerations**:
- Requires modifying application code to handle TLS
- Need to implement certificate loading and validation
- Must handle certificate rotation and renewal
- Increases application complexity and maintenance burden

**Use Cases**:
- Specialized applications with unique TLS requirements
- Embedded systems with resource constraints
- Applications requiring custom TLS handshake logic

## Recommendation: Caddy Server

### Why Caddy?

After evaluating all options, **Caddy Server** is the recommended solution for the following reasons:

#### 1. **Developer Experience**
- Zero-configuration HTTPS for local development with automatic self-signed certificates
- Developers can start working with HTTPS immediately without manual certificate generation
- Consistent experience between development and production environments

#### 2. **Flexible Certificate Options**
- Automatic self-signed certificates for quick development setup
- Support for user-provided certificates for production deployments
- Simple configuration for both certificate types
- Reduces setup complexity and configuration errors

#### 3. **Simplicity**
- Minimal configuration required (often just a few lines)
- Human-readable Caddyfile format
- Reduces configuration errors and security misconfigurations

#### 4. **Modern Standards**
- HTTP/2 and HTTP/3 support by default
- Secure TLS defaults (TLS 1.2+ with strong cipher suites)
- Automatic security best practices

#### 5. **Container-Friendly**
- Small footprint suitable for containerized deployments
- Easy to integrate with Podman/Docker workflows
- Aligns with the project's existing container-based architecture

#### 6. **Flexibility**
- Works seamlessly in development (self-signed) and production (user-provided certificates)
- Can be deployed as a sidecar container or standalone reverse proxy
- Supports both local and cloud deployments

#### 7. **Maintenance**
- Minimal ongoing maintenance required
- Automatic updates to security configurations
- Less operational burden compared to nginx + certbot

### Example Implementation

The project already has Caddy configuration templates in place:
- `ai-services/assets/catalog-test/podman/templates/Caddyfile`
- `ai-services/assets/catalog-test/podman/templates/caddy.yaml`

These can be extended to support both development and production scenarios.

## Implementation

This section provides step-by-step instructions for integrating Caddy server with the Catalog service to enable HTTPS.

**Note:** The Caddy ppc64le image is available in Docker, but needs to be built with UBI (Universal Base Image) for this project.

### Step 1: Deploy Caddy Server with Catalog Assets

As part of the `ai-services catalog configure` command, deploy the Caddy server alongside other Catalog assets.

**Command Options:**
```bash
ai-services catalog configure [options]
  --domain-name <domain>     Custom domain name for self-signed certificates (optional)
                             If not provided, uses wildcard DNS format: <service>.<ip>.nip.io
                             Example: --domain-name example.com generates certs for *.example.com
  --external-port <port>     External HTTPS port for Caddy server (optional, default: 443)
                             If not 443, port will be included in service URLs
                             Example: --external-port 8443 results in https://service.<domain>:8443
  --ssl-cert <path>          Path to user-provided SSL certificate (optional)
  --ssl-key <path>           Path to user-provided SSL private key (optional)
```

**Process:**
1. Before installing Catalog assets, create a minimal Caddyfile configuration
2. Write the Caddyfile to `/var/lib/ai-services/certs/Caddyfile`
3. Determine certificate strategy:
   - If `--ssl-cert` and `--ssl-key` are provided, load user-provided certificates into Caddy
   - If `--domain-name` is provided, generate self-signed certificates for the specified domain
   - If neither is provided, use wildcard DNS format `<service>.<ip>.nip.io` with self-signed certificates
4. **Populate configuration in Catalog:**
   - Update the `values.yaml` file with the domain name (from `--domain-name` flag or extracted from certificate)
   - Update the `values.yaml` file with the external port (from `--external-port` flag, default: 443)
   - The Catalog backend will use these values when generating service URLs and registering routes with Caddy
   - If port is not 443, URLs will include the port: `https://service.domain:port`
5. Deploy Caddy container along with Catalog service containers

**Minimal Caddyfile Configuration:**

```caddyfile
{
    admin 0.0.0.0:2019

    servers :443 {
        name my_app_server
    }
}

:443 {
    tls internal
}


# This file will be used as the base configuration
# Routes will be dynamically added via Caddy Admin API
```

**Why this minimal configuration?**
- Sets up Caddy Admin API on port 2019 for dynamic route management
- Configures server named `my_app_server` on port 443
- Uses internal self-signed certificates with on-demand generation
- Provides a clean base for dynamic route registration

**Loading User-Provided Certificates:**

If the user provides custom certificates via `--ssl-cert` and `--ssl-key` flags, load them into Caddy using the Admin API:

```go
// Load user-provided certificates into Caddy with validation
func LoadUserCertificates(certPath, keyPath string) error {
    // Step 1: Read certificate and key files
    certBytes, err := os.ReadFile(certPath)
    if err != nil {
        return fmt.Errorf("failed to read certificate: %w", err)
    }
    
    keyBytes, err := os.ReadFile(keyPath)
    if err != nil {
        return fmt.Errorf("failed to read private key: %w", err)
    }
    
    // Step 2: Parse x509 certificate
    certBlock, _ := pem.Decode(certBytes)
    if certBlock == nil {
        return fmt.Errorf("failed to decode certificate PEM")
    }
    
    cert, err := x509.ParseCertificate(certBlock.Bytes)
    if err != nil {
        return fmt.Errorf("failed to parse certificate: %w", err)
    }
    
    // Step 3: Validate certificate
    
    // 3a. Check for wildcard SAN (required for multiple subdomains)
    hasWildcard := false
    for _, dnsName := range cert.DNSNames {
        if strings.HasPrefix(dnsName, "*.") {
            hasWildcard = true
            break
        }
    }
    if !hasWildcard {
        return fmt.Errorf("certificate must contain wildcard SAN entry (e.g., *.example.com)")
    }
    
    // 3b. Check certificate expiry
    now := time.Now()
    if now.Before(cert.NotBefore) {
        return fmt.Errorf("certificate is not yet valid (valid from: %s)", cert.NotBefore)
    }
    if now.After(cert.NotAfter) {
        return fmt.Errorf("certificate has expired (expired on: %s)", cert.NotAfter)
    }
    
    // Warn if certificate expires soon (within 30 days)
    daysUntilExpiry := cert.NotAfter.Sub(now).Hours() / 24
    if daysUntilExpiry < 30 {
        log.Printf("Warning: Certificate expires in %.0f days (%s)", daysUntilExpiry, cert.NotAfter)
    }
    
    // 3c. Verify key pair match
    keyBlock, _ := pem.Decode(keyBytes)
    if keyBlock == nil {
        return fmt.Errorf("failed to decode private key PEM")
    }
    
    privateKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
    if err != nil {
        // Try PKCS1 format
        privateKey, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
        if err != nil {
            return fmt.Errorf("failed to parse private key: %w", err)
        }
    }
    
    // Verify the public key in cert matches the private key
    switch pub := cert.PublicKey.(type) {
    case *rsa.PublicKey:
        priv, ok := privateKey.(*rsa.PrivateKey)
        if !ok {
            return fmt.Errorf("private key type does not match certificate public key type")
        }
        if pub.N.Cmp(priv.N) != 0 {
            return fmt.Errorf("private key does not match certificate")
        }
    case *ecdsa.PublicKey:
        priv, ok := privateKey.(*ecdsa.PrivateKey)
        if !ok {
            return fmt.Errorf("private key type does not match certificate public key type")
        }
        if pub.X.Cmp(priv.X) != 0 || pub.Y.Cmp(priv.Y) != 0 {
            return fmt.Errorf("private key does not match certificate")
        }
    default:
        return fmt.Errorf("unsupported public key type")
    }
    
    // Step 4 (Optional): Verify certificate chain
    // This step validates that the certificate is signed by a trusted CA
    // Uncomment if chain verification is required
    /*
    roots := x509.NewCertPool()
    // Add system root CAs or custom CA certificates
    roots.AppendCertsFromPEM([]byte(customCAcert))
    
    opts := x509.VerifyOptions{
        Roots: roots,
        DNSName: "", // Can specify expected DNS name
    }
    
    if _, err := cert.Verify(opts); err != nil {
        return fmt.Errorf("certificate chain verification failed: %w", err)
    }
    */
    
    // Step 5: Load certificates via Caddy Admin API
    // Extract domain from certificate for ID
    domain := ""
    for _, dnsName := range cert.DNSNames {
        if strings.HasPrefix(dnsName, "*.") {
            domain = strings.TrimPrefix(dnsName, "*.")
            break
        }
    }
    if domain == "" {
        return fmt.Errorf("no wildcard domain found in certificate")
    }
    
    payload := map[string]interface{}{
        "@id": fmt.Sprintf("cert-%s", domain),
        "load_pem": []map[string]string{
            {
                "certificate": string(certBytes),
                "key":         string(keyBytes),
            },
        },
    }
    
    data, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("failed to marshal payload: %w", err)
    }
    
    resp, err := http.Post(
        "http://localhost:2019/config/apps/tls/certificates/load_pem",
        "application/json",
        bytes.NewBuffer(data),
    )
    if err != nil {
        return fmt.Errorf("failed to load certificates: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("caddy returned error: %s", string(body))
    }
    
    log.Printf("Successfully loaded certificate (expires: %s)", cert.NotAfter)
    return nil
}
```

**Certificate Validation Steps:**

1. **Read certificate and key files** - Load PEM-encoded files from disk
2. **Parse x509 certificate** - Decode and parse certificate structure
3. **Validate certificate:**
   - **SAN wildcard check** - Ensures certificate has wildcard entry (e.g., `*.example.com`)
   - **Expiry check** - Verifies certificate is currently valid and warns if expiring soon
   - **Key pair match** - Confirms private key corresponds to certificate's public key
4. **Verify certificate chain (optional)** - Validates certificate is signed by trusted CA
5. **Load via Caddy Admin API** - Upload validated certificate to Caddy

**Validation Benefits:**
- Prevents loading invalid or expired certificates
- Catches key/certificate mismatches before deployment
- Provides early warning for expiring certificates
- Ensures wildcard SAN requirement is met

**Certificate Loading Flow:**

1. **User provides certificates:**
   ```bash
   ai-services catalog configure --ssl-cert /path/to/cert.pem --ssl-key /path/to/key.pem
   ```

2. **System reads and validates certificate files:**
   - Validates that both certificate and key files exist
   - Reads file contents into memory
   - **Extracts domain name from certificate's Common Name (CN) or Subject Alternative Name (SAN)**
   - **Verifies SAN contains wildcard entry** (e.g., `*.example.com`) to support multiple subdomains

3. **Load into Caddy via Admin API:**
   - POST to `http://localhost:2019/config/apps/tls/certificates/load_pem`
   - Payload contains certificate and key as strings
   - Caddy validates and stores the certificates

4. **Certificates are used automatically:**
   - Caddy uses loaded certificates for all HTTPS connections
   - No need to restart Caddy container
   - Certificates are immediately available for new routes

**Certificate Domain Name Extraction:**

```go
// Extract domain name from certificate
func ExtractDomainFromCert(certPath string) (string, error) {
    certPEM, err := os.ReadFile(certPath)
    if err != nil {
        return "", fmt.Errorf("failed to read certificate: %w", err)
    }
    
    block, _ := pem.Decode(certPEM)
    if block == nil {
        return "", fmt.Errorf("failed to decode PEM block")
    }
    
    cert, err := x509.ParseCertificate(block.Bytes)
    if err != nil {
        return "", fmt.Errorf("failed to parse certificate: %w", err)
    }
    
    // Check for wildcard in SAN - REQUIRED for multiple services
    for _, dnsName := range cert.DNSNames {
        if strings.HasPrefix(dnsName, "*.") {
            // Extract base domain from wildcard (e.g., *.example.com -> example.com)
            return strings.TrimPrefix(dnsName, "*."), nil
        }
    }
    
    // No wildcard found - reject the certificate
    return "", fmt.Errorf("certificate MUST contain wildcard SAN entry (e.g., *.example.com) to support multiple subdomains like catalog-api, rag-demo-chat-bot-ui, etc. Current certificate does not have wildcard SAN")
}
```

**Domain Selection Logic:**

When registering routes with Caddy, the domain format depends on whether user-provided certificates are used:

| Certificate Type | Domain Format | Example |
|-----------------|---------------|---------|
| Self-signed (default) | `<pod_name>-<container_name>.<ip>.nip.io` | `ai-services--catalog-ui.10.20.186.33.nip.io` |
| User-provided | `<pod_name>-<container_name>.<domain>` | `ai-services--catalog-ui.example.com` |

**Implementation:**

```go
func GetDomainForService(podName, containerName, hostIP, domainName, userCertPath string) (string, error) {
    // Priority 1: User-provided certificates (extract domain name from cert)
    if userCertPath != "" {
        domain, err := ExtractDomainFromCert(userCertPath)
        if err != nil {
            return "", fmt.Errorf("failed to extract domain name from certificate: %w", err)
        }
        return fmt.Sprintf("%s-%s.%s", podName, containerName, domain), nil
    }
    
    // Priority 2: Custom domain name for self-signed certificates
    if domainName != "" {
        return fmt.Sprintf("%s-%s.%s", podName, containerName, domainName), nil
    }
    
    // Priority 3: Default wildcard DNS with nip.io
    return fmt.Sprintf("%s-%s.%s.nip.io", podName, containerName, hostIP), nil
}
```

**Certificate SAN Requirements:**

For user-provided certificates to work with multiple services, the certificate **must** include a wildcard entry in the Subject Alternative Name (SAN) field:

```
Subject Alternative Name:
    DNS: *.example.com
    DNS: example.com
```

This allows the certificate to be valid for:
- `ai-services--catalog-ui.example.com`
- `rag-demo-chat-bot-ui.example.com`
- `rag-demo-digitize-api.example.com`
- Any other `<service>.example.com` subdomain

**Example Certificate Generation with Wildcard SAN:**

```bash
# Generate private key
openssl genrsa -out key.pem 2048

# Create certificate signing request with SAN
openssl req -new -key key.pem -out cert.csr \
  -subj "/CN=example.com" \
  -addext "subjectAltName=DNS:*.example.com,DNS:example.com"

# Self-sign the certificate
openssl x509 -req -in cert.csr -signkey key.pem -out cert.pem \
  -days 365 -copy_extensions copy
```

**Benefits of this approach:**
- No need to mount certificate files into Caddy container
- Certificates can be updated without container restart
- Centralized certificate management via Admin API
- Works seamlessly with both self-signed and user-provided certificates

### Step 2: Register Catalog Route via Caddy Admin API

After deploying the Catalog assets, dynamically register the Catalog service route using the Caddy Admin API.

**Domain Format:**

The domain format depends on the certificate configuration:

- **Default (no flags):** `<service>.<ip>.nip.io`
  - Example: `ai-services--catalog-ui.10.20.186.33.nip.io`
  - Uses wildcard DNS service for automatic IP resolution
  - Self-signed certificates generated automatically

- **With `--domain-name` flag:** `<service>.<domain>`
  - Example: `ai-services--catalog-ui.example.com` (when `--domain-name example.com`)
  - Self-signed certificates generated for `*.example.com`
  - Requires DNS configuration to point domain to host IP

- **With user-provided certificates:** `<service>.<domain>`
  - Example: `ai-services--catalog-ui.example.com`
  - Domain name extracted from certificate's SAN wildcard entry
  - Uses provided certificates instead of generating new ones

**Certificate Configuration Strategies:**

**1. Default Strategy (nip.io):**

[nip.io](https://nip.io) is a wildcard DNS service that provides automatic DNS resolution for any IP address. It eliminates the need for:
- Manual DNS configuration during development
- Editing `/etc/hosts` files
- Setting up local DNS servers
- Managing DNS records for testing

**How nip.io works:**
- Any request to `<anything>.<ip>.nip.io` automatically resolves to `<ip>`
- Example: `ai-services--catalog-ui.10.20.186.33.nip.io` → resolves to `10.20.186.33`
- Enables HTTPS with proper domain names without DNS infrastructure
- Perfect for development, testing, and demo environments

**2. Custom Domain Strategy (`--domain-name`):**

When users specify a custom domain for self-signed certificates:
- Self-signed certificates are generated for `*.<domain>`
- Example: `--domain-name example.com` generates certs for `*.example.com`
- Services are accessible at `<service>.example.com`
- Requires DNS configuration to point the domain to the host IP
- Useful for internal networks with custom DNS infrastructure
- Provides branded domain names for development/staging environments

**3. User-Provided Certificate Strategy (`--ssl-cert`/`--ssl-key`):**

**Why domain name extraction for user-provided certificates?**

When users provide their own certificates:
- The certificate is issued for a specific domain (e.g., `*.example.com`)
- We must use that domain when registering routes with Caddy
- Using nip.io would cause certificate validation errors
- The domain name is extracted from the certificate's SAN wildcard entry
- All services use subdomains of the extracted domain name

**Caddy Admin API Call:**

```bash
curl http://localhost:2019/config/apps/http/servers/my_app_server/routes \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "@id": "route-ai-services--catalog-ui",
    "match": [
      {
        "host": ["ai-services--catalog-ui.<ip>.nip.io"]
      }
    ],
    "handle": [
      {
        "handler": "reverse_proxy",
        "upstreams": [
          {
            "dial": "ai-services--catalog:8081"
          }
        ]
      }
    ],
    "terminal": true
  }'
```

**Parameters:**
- `@id`: Unique identifier for the route in format `route-<pod_name>-<container_name>` (e.g., `route-ai-services--catalog-ui`)
- `host`: The domain pattern matching `<pod_name>-<container_name>.<ip>.nip.io`
- `dial`: The internal service address (container name and port)
- `terminal`: Stops route matching after this route is matched

### Step 3: Display in Catalog Next Steps

After successful deployment, the Catalog service should display the HTTPS endpoint in the "Next Steps" output.

**Example Output:**

**Default Configuration (nip.io):**
```
✓ Catalog service deployed successfully

Next Steps:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. Access the Catalog API via HTTPS:
   
   https://ai-services--catalog-ui.10.20.186.33.nip.io
   
   Note: Using self-signed certificate with wildcard DNS (nip.io).
         Your browser may show a security warning.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

**With Custom Domain (`--domain-name example.com`):**
```
✓ Catalog service deployed successfully

Next Steps:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. Access the Catalog API via HTTPS:
   
   https://ai-services--catalog-ui.example.com
   
   Note: Using self-signed certificate for *.example.com.
         Ensure DNS is configured to point example.com to 10.20.186.33.
         Your browser may show a security warning.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

**With User-Provided Certificates:**
```
✓ Catalog service deployed successfully

Next Steps:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. Access the Catalog API via HTTPS:
   
   https://ai-services--catalog-ui.example.com
   
   Note: Using user-provided certificate.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### Step 4: Enable HTTPS for Individual Applications

As part of the `ai-services application create` command, applications can be automatically exposed via HTTPS by registering their routes with Caddy.

**Process:**
1. During application deployment, scan for services with the annotation `ai-services.io/ports`
2. For each annotated service, make a Caddy Admin API call to register the route
3. Display HTTPS endpoints in the application's "Next Steps" output

**Domain Format:**
```
<pod_name>-<container_name>.<ip>.nip.io
```

**Identifying Services to Expose:**

Services that need HTTPS exposure should have the annotation:
```yaml
metadata:
  annotations:
    ai-ai-services.io/ports: "8080,3000"  # Comma-separated list of ports
```

**Dynamic Route Registration:**

For each port in the annotation, register a route with Caddy:

```bash
# Example for a service in pod "rag-demo" with container "chat-bot" on port 3000
curl http://caddy:2019/config/apps/http/servers/my_app_server/routes \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "@id": "route-<pod_name>-<container_name>",
    "match": [
      {
        "host": ["<pod_name>-<container_name>.<ip>.nip.io"]
      }
    ],
    "handle": [
      {
        "handler": "reverse_proxy",
        "upstreams": [
          {
            "dial": "<pod_name>-<container_name>:<port>"
          }
        ]
      }
    ],
    "terminal": true
  }'
```

**Example Scenario:**

Pod: `rag-demo-chat-bot`
Container: `ui` with annotation `ai-ai-services.io/ports: "3000"`

**Caddy Admin API Call:**
```bash
curl http://caddy:2019/config/apps/http/servers/my_app_server/routes \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "@id": "route-rag-demo-chat-bot-ui",
    "match": [
      {
        "host": ["rag-demo-chat-bot-ui.10.20.186.33.nip.io"]
      }
    ],
    "handle": [
      {
        "handler": "reverse_proxy",
        "upstreams": [
          {
            "dial": "rag-demo-chat-bot:3000"
          }
        ]
      }
    ],
    "terminal": true
  }'
```

**Application Next Steps Output:**

After successful application deployment, display HTTPS endpoints for all exposed services:

```
✓ Application 'rag-demo' deployed successfully

Next Steps:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. Access your application services via HTTPS:
   
   Chat Bot UI:  https://rag-demo-chat-bot-ui.10.20.186.33.nip.io
   Digitize API: https://rag-demo-digitize-api.10.20.186.33.nip.io
   
   Note: Using self-signed certificates. Your browser may show a security warning.


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

**Implementation Logic:**

```go
// Pseudocode for application HTTPS enablement
func EnableHTTPSForApplication(podName string, containers []Container, hostIP string) error {
    for _, container := range containers {
        // Check for ports annotation
        portsAnnotation := container.Annotations["ai-services.io/ports"]
        if portsAnnotation == "" {
            continue // Skip containers without the annotation
        }
        
        // Parse ports from annotation
        ports := strings.Split(portsAnnotation, ",")
        
        for _, port := range ports {
            // Construct domain name: <pod_name>-<container_name>.<ip>.nip.io
            domain := fmt.Sprintf("%s-%s.%s.nip.io", podName, container.Name, hostIP)
            
            // Construct service address: <pod_name>-<container_name>:<port>
            serviceAddr := fmt.Sprintf("%s-%s:%s", podName, container.Name, strings.TrimSpace(port))
            
            // Register route with Caddy
            err := registerCaddyRoute(domain, serviceAddr)
            if err != nil {
                return fmt.Errorf("failed to register route for %s: %w", container.Name, err)
            }
            
            // Add to Next Steps output
            addToNextSteps(container.Name, domain)
        }
    }
    return nil
}
```

**Benefits:**
- Automatic HTTPS enablement for application services
- Consistent domain naming pattern: `<pod_name>-<container_name>.<ip>.nip.io`
- No manual Caddy configuration required per application
- Self-signed certificates work immediately for development
- Easy transition to production certificates

### Step 6: Trusting Self-Signed Certificates (Optional)

When using Caddy's self-signed certificates, browsers will show security warnings. To avoid these warnings in development, install Caddy's root CA certificate.

**Root CA Certificate Location:**
```
/var/lib/ai-services/data/caddy/pki/authorities/local/root.crt
```

**Installation:**
- **macOS:** `sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain root.crt`
- **Linux:** Copy to `/usr/local/share/ca-certificates/` and run `sudo update-ca-certificates`
- **Windows:** `certutil -addstore -f "ROOT" root.crt`

**Alternative:** Use `curl -k` to skip certificate verification for testing.

**Note:** This step is only needed for self-signed certificates. User-provided certificates from trusted CAs don't require this.

## Conclusion

**Caddy Server** provides the optimal balance of simplicity, security, and functionality for the AI Services project. Its automatic HTTPS capabilities significantly reduce operational overhead while maintaining production-grade security. The existing Caddy configuration in the project demonstrates that this approach is already being adopted, and this proposal formalizes that decision with comprehensive justification.

For development environments, Caddy's automatic self-signed certificates enable immediate HTTPS testing without manual certificate generation. For production, users can provide their own certificates (from their organization's CA or purchased from certificate authorities), which Caddy seamlessly integrates with minimal configuration.

This approach aligns with modern DevOps practices, reduces maintenance burden, and provides a consistent security posture across all deployment environments.
