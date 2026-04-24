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

### Step 1: Deploy Caddy Server with Catalog Assets

As part of the `ai-services catalog configure` command, deploy the Caddy server alongside other Catalog assets.

**Command Options:**
```bash
ai-services catalog configure [options]
  --ssl-cert <path>    Path to user-provided SSL certificate (optional)
  --ssl-key <path>     Path to user-provided SSL private key (optional)
```

**Process:**
1. Before installing Catalog assets, create a minimal Caddyfile configuration
2. Write the Caddyfile to `/var/lib/ai-services/certs/Caddyfile`
3. If user provides certificates via `--ssl-cert` and `--ssl-key`, load them into Caddy
4. Deploy Caddy container along with Catalog service containers

**Minimal Caddyfile Configuration:**

```caddyfile
{
    admin 0.0.0.0:2019

    servers :443 {
        name my_app_server
    }
}

:443 {
    tls internal {
        on_demand
    }
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
// Load user-provided certificates into Caddy
func LoadUserCertificates(certPath, keyPath string) error {
    // Read certificate and key files
    certBytes, err := os.ReadFile(certPath)
    if err != nil {
        return fmt.Errorf("failed to read certificate: %w", err)
    }
    
    keyBytes, err := os.ReadFile(keyPath)
    if err != nil {
        return fmt.Errorf("failed to read private key: %w", err)
    }
    
    // Prepare payload for Caddy Admin API
    payload := []map[string]string{
        {
            "certificate": string(certBytes),
            "key":         string(keyBytes),
        },
    }
    
    data, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("failed to marshal payload: %w", err)
    }
    
    // Load certificates via Caddy Admin API
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
    
    return nil
}
```

**Certificate Loading Flow:**

1. **User provides certificates:**
   ```bash
   ai-services catalog configure --ssl-cert /path/to/cert.pem --ssl-key /path/to/key.pem
   ```

2. **System reads certificate files:**
   - Validates that both certificate and key files exist
   - Reads file contents into memory

3. **Load into Caddy via Admin API:**
   - POST to `http://localhost:2019/config/apps/tls/certificates/load_pem`
   - Payload contains certificate and key as strings
   - Caddy validates and stores the certificates

4. **Certificates are used automatically:**
   - Caddy uses loaded certificates for all HTTPS connections
   - No need to restart Caddy container
   - Certificates are immediately available for new routes

**Benefits of this approach:**
- No need to mount certificate files into Caddy container
- Certificates can be updated without container restart
- Centralized certificate management via Admin API
- Works seamlessly with both self-signed and user-provided certificates

### Step 2: Register Catalog Route via Caddy Admin API

After deploying the Catalog assets, dynamically register the Catalog service route using the Caddy Admin API.

**Domain Format:**
```
<pod_name>-<container_name>.<ip>.nip.io
```

**Example:** `ai-services--catalog-ui.<ip>.nip.io`

**Why nip.io?**

[nip.io](https://nip.io) is a wildcard DNS service that provides automatic DNS resolution for any IP address. It eliminates the need for:
- Manual DNS configuration during development
- Editing `/etc/hosts` files
- Setting up local DNS servers
- Managing DNS records for testing

**How nip.io works:**
- Any request to `<anything>.<ip>.nip.io` automatically resolves to `<ip>`
- Example: `ai-services--catalog-ui.<ip>.nip.io` → resolves to `10.20.186.33`
- Enables HTTPS with proper domain names without DNS infrastructure
- Perfect for development, testing, and demo environments

**Caddy Admin API Call:**

```bash
curl http://localhost:2019/config/apps/http/servers/my_app_server/routes \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
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
- `host`: The domain pattern matching `<pod_name>-<container_name>.<ip>.nip.io`
- `dial`: The internal service address (container name and port)
- `terminal`: Stops route matching after this route is matched

### Step 3: Display in Catalog Next Steps

After successful deployment, the Catalog service should display the HTTPS endpoint in the "Next Steps" output.

**Example Output:**

```
✓ Catalog service deployed successfully

Next Steps:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. Access the Catalog API via HTTPS:
   
   https://catalog-api.10.20.186.33.nip.io
   
   Note: Using self-signed certificate. Your browser may show a security warning.


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### Step 4: Enable HTTPS for Individual Applications

As part of the `ai-services application create` command, applications can be automatically exposed via HTTPS by registering their routes with Caddy.

**Process:**
1. During application deployment, scan for services with the annotation `ai-ai-services.io/ports`
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
curl http://localhost:2019/config/apps/http/servers/my_app_server/routes \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
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
        portsAnnotation := container.Annotations["ai-ai-services.io/ports"]
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

## Conclusion

**Caddy Server** provides the optimal balance of simplicity, security, and functionality for the AI Services project. Its automatic HTTPS capabilities significantly reduce operational overhead while maintaining production-grade security. The existing Caddy configuration in the project demonstrates that this approach is already being adopted, and this proposal formalizes that decision with comprehensive justification.

For development environments, Caddy's automatic self-signed certificates enable immediate HTTPS testing without manual certificate generation. For production, users can provide their own certificates (from their organization's CA or purchased from certificate authorities), which Caddy seamlessly integrates with minimal configuration.

This approach aligns with modern DevOps practices, reduces maintenance burden, and provides a consistent security posture across all deployment environments.
