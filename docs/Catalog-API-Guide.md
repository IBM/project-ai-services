# AI Services Catalog API Documentation Guide

**Version:** 1.0  
**Last Updated:** 2026-02-26  
**Audience:** UI Developers, QA Teams, Integration Partners, API Consumers

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [System Architecture](#system-architecture)
3. [API Overview](#api-overview)
4. [Authentication](#authentication)
5. [Available Endpoints](#available-endpoints)
6. [Using Swagger UI](#using-swagger-ui)
7. [Integration Guide](#integration-guide)
8. [Common Workflows](#common-workflows)
9. [Error Handling](#error-handling)
10. [Best Practices](#best-practices)
11. [Troubleshooting](#troubleshooting)
12. [Developer Reference](#developer-reference)

---

## Quick Start

### Accessing the API Documentation

**Swagger UI:** http://localhost:8080/swagger/index.html  
**OpenAPI Spec (JSON):** http://localhost:8080/swagger/doc.json  
**Health Check:** http://localhost:8080/healthz

### Basic Authentication Flow

```bash
# 1. Login to get tokens
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your-password"}'

# Response:
{
  "access_token": "eyJhbGc...",
  "refresh_token": "eyJhbGc...",
  "token_type": "Bearer"
}

# 2. Use access token for protected endpoints
curl -X GET http://localhost:8080/api/v1/auth/me \
  -H "Authorization: Bearer eyJhbGc..."
```

---

## System Architecture

### Overview Diagram

```mermaid
graph TB
    subgraph "Client Browser"
        UI[Swagger UI]
    end
    
    subgraph "Catalog API Server Port 8080"
        Router[Gin Router]
        SwaggerHandler[Swagger Handler]
        AuthHandler[Auth Handler]
        AppHandler[Application Handler]
        Middleware[Auth Middleware]
        
        Router --> SwaggerHandler
        Router --> AuthHandler
        Router --> AppHandler
        Router --> Middleware
        Middleware --> AuthHandler
        Middleware --> AppHandler
    end
    
    subgraph "Generated Documentation"
        DocsGo[docs/docs.go]
        SwaggerJSON[docs/swagger.json]
        SwaggerYAML[docs/swagger.yaml]
    end
    
    subgraph "Source Code with Annotations"
        MainGo[cmd/ai-services/main.go]
        APIServer[apiserver.go]
        AuthHandlerGo[auth_handler.go]
        RouterGo[router.go]
    end
    
    UI -->|HTTP GET /swagger/index.html| SwaggerHandler
    SwaggerHandler -->|Serves| DocsGo
    SwaggerHandler -->|Serves| SwaggerJSON
    
    MainGo -.->|swag init| DocsGo
    APIServer -.->|swag init| DocsGo
    AuthHandlerGo -.->|swag init| DocsGo
    RouterGo -.->|swag init| DocsGo
    
    DocsGo -.->|generates| SwaggerJSON
    DocsGo -.->|generates| SwaggerYAML
    
    UI -->|Test API| Router
    Router -->|auth endpoints| AuthHandler
    Router -->|application endpoints| AppHandler

    style UI fill:#e1f5ff
    style SwaggerHandler fill:#fff4e1
    style DocsGo fill:#e8f5e9
    style SwaggerJSON fill:#e8f5e9
    style SwaggerYAML fill:#e8f5e9
```

---

## API Overview

### Base Information

- **Title:** AI Services Catalog API
- **Version:** 1.0
- **Base URL:** http://localhost:8080/api/v1
- **Protocol:** HTTP/HTTPS
- **Authentication:** Bearer Token (JWT)
- **Content-Type:** application/json

### API Categories

The API is organized into two main categories:

1. **Authentication** - User authentication and authorization
2. **Applications** - Application lifecycle management

---

## Authentication

### Overview

The API uses JWT (JSON Web Token) based authentication with Bearer tokens.

**Token Types:**
- **Access Token:** Short-lived (15 minutes), used for API requests
- **Refresh Token:** Long-lived (7 days), used to obtain new access tokens

### Authentication Endpoints

#### 1. Login
**Endpoint:** `POST /api/v1/auth/login`  
**Description:** Authenticate user and receive tokens  
**Authentication:** None required

**Request:**
```json
{
  "username": "admin",
  "password": "your-password"
}
```

**Response (200 OK):**
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer"
}
```

**Errors:**
- `400` - Invalid payload
- `401` - Invalid credentials

#### 2. Refresh Token
**Endpoint:** `POST /api/v1/auth/refresh`  
**Description:** Get new tokens using refresh token  
**Authentication:** None required

**Request:**
```json
{
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**Response (200 OK):**
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer"
}
```

**Errors:**
- `400` - Invalid payload
- `401` - Invalid refresh token

#### 3. Logout
**Endpoint:** `POST /api/v1/auth/logout`  
**Description:** Invalidate current access token  
**Authentication:** Required (Bearer token)

**Headers:**
```
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

**Response (200 OK):**
```json
{
  "message": "logged out"
}
```

**Errors:**
- `400` - Missing token
- `500` - Failed to logout

#### 4. Get Current User
**Endpoint:** `GET /api/v1/auth/me`  
**Description:** Get information about authenticated user  
**Authentication:** Required (Bearer token)

**Headers:**
```
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

**Response (200 OK):**
```json
{
  "id": "uid_1",
  "username": "admin",
  "name": "Admin"
}
```

**Errors:**
- `401` - Unauthorized
- `404` - User not found

---

## Available Endpoints

### Application Management

All application endpoints are currently placeholders and will return:
```json
{
  "message": "This is a placeholder endpoint for /api/v1/applications/..."
}
```
## Using Swagger UI

### Accessing Swagger UI

1. Open browser to: http://localhost:8080/swagger/index.html
2. You'll see two sections: **Authentication** and **Applications**

### Testing Authentication Flow

#### Step 1: Login

1. Expand **Authentication** section
2. Click **POST /api/v1/auth/login**
3. Click **"Try it out"**
4. Enter credentials:
   ```json
   {
     "username": "admin",
     "password": "your-password"
   }
   ```
5. Click **"Execute"**
6. Copy the `access_token` from the response

#### Step 2: Authorize

1. Click the **"Authorize"** button (lock icon at top right)
2. In the "Value" field, enter: `Bearer <your-access-token>`
   - Example: `Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...`
   - **Important:** Include "Bearer " prefix with a space
3. Click **"Authorize"**
4. Click **"Close"**

#### Step 3: Test Protected Endpoints

1. Expand **GET /api/v1/auth/me**
2. Click **"Try it out"**
3. Click **"Execute"**
4. You should see your user information

### Important Notes

- **Manual Token Management:** Swagger UI requires manual copy-paste of tokens (this is standard behavior)
- **Token Expiration:** Access tokens expire after 15 minutes
- **Refresh Tokens:** Use the refresh endpoint to get new tokens without re-login
- **Lock Icons:** Closed lock icons indicate endpoints require authentication

---

## Integration Guide

### For Frontend Developers

#### JavaScript/TypeScript Example

```typescript
// Login function
async function login(username: string, password: string) {
  const response = await fetch('http://localhost:8080/api/v1/auth/login', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ username, password }),
  });
  
  if (!response.ok) {
    throw new Error('Login failed');
  }
  
  const data = await response.json();
  // Store tokens securely
  localStorage.setItem('access_token', data.access_token);
  localStorage.setItem('refresh_token', data.refresh_token);
  
  return data;
}

// API call with authentication
async function getCurrentUser() {
  const token = localStorage.getItem('access_token');
  
  const response = await fetch('http://localhost:8080/api/v1/auth/me', {
    method: 'GET',
    headers: {
      'Authorization': `Bearer ${token}`,
    },
  });
  
  if (!response.ok) {
    if (response.status === 401) {
      // Token expired, try refresh
      await refreshToken();
      return getCurrentUser(); // Retry
    }
    throw new Error('Failed to get user');
  }
  
  return response.json();
}

// Refresh token function
async function refreshToken() {
  const refreshToken = localStorage.getItem('refresh_token');
  
  const response = await fetch('http://localhost:8080/api/v1/auth/refresh', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ refresh_token: refreshToken }),
  });
  
  if (!response.ok) {
    // Refresh failed, redirect to login
    window.location.href = '/login';
    throw new Error('Session expired');
  }
  
  const data = await response.json();
  localStorage.setItem('access_token', data.access_token);
  localStorage.setItem('refresh_token', data.refresh_token);
  
  return data;
}
```

#### React Hook Example

```typescript
import { useState, useEffect } from 'react';

export function useAuth() {
  const [user, setUser] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    checkAuth();
  }, []);

  async function checkAuth() {
    const token = localStorage.getItem('access_token');
    if (!token) {
      setLoading(false);
      return;
    }

    try {
      const response = await fetch('http://localhost:8080/api/v1/auth/me', {
        headers: { 'Authorization': `Bearer ${token}` },
      });
      
      if (response.ok) {
        const userData = await response.json();
        setUser(userData);
      }
    } catch (error) {
      console.error('Auth check failed:', error);
    } finally {
      setLoading(false);
    }
  }

  return { user, loading, checkAuth };
}
```

## Common Workflows

### Request Flow Diagram

```mermaid
sequenceDiagram
    participant User
    participant Browser
    participant SwaggerUI as Swagger UI
    participant APIServer as API Server
    participant Handler as Auth Handler
    
    User->>Browser: Navigate to /swagger/index.html
    Browser->>APIServer: GET /swagger/index.html
    APIServer->>SwaggerUI: Serve Swagger UI
    SwaggerUI->>APIServer: GET /swagger/doc.json
    APIServer->>SwaggerUI: Return OpenAPI spec
    SwaggerUI->>Browser: Render API documentation
    Browser->>User: Display interactive docs
    
    Note over User,Handler: User tests Login endpoint
    
    User->>SwaggerUI: Click "Try it out" on /auth/login
    User->>SwaggerUI: Enter credentials
    SwaggerUI->>APIServer: POST /api/v1/auth/login
    APIServer->>Handler: Process login
    Handler->>APIServer: Return tokens
    APIServer->>SwaggerUI: 200 OK with tokens
    SwaggerUI->>User: Display response
    
    Note over User,Handler: User authorizes with token
    
    User->>SwaggerUI: Click "Authorize" button
    User->>SwaggerUI: Enter "Bearer <token>"
    SwaggerUI->>SwaggerUI: Store authorization
    
    Note over User,Handler: User tests protected endpoint
    
    User->>SwaggerUI: Click "Try it out" on /auth/me
    SwaggerUI->>APIServer: GET /api/v1/auth/me with Bearer token
    APIServer->>Handler: Validate token & get user
    Handler->>APIServer: Return user info
    APIServer->>SwaggerUI: 200 OK with user data
    SwaggerUI->>User: Display response
```

### Workflow 1: User Login and API Access

```
1. User enters credentials
   ↓
2. Frontend calls POST /api/v1/auth/login
   ↓
3. Backend validates credentials
   ↓
4. Backend returns access_token and refresh_token
   ↓
5. Frontend stores tokens (localStorage/sessionStorage)
   ↓
6. Frontend includes token in subsequent requests:
   Header: Authorization: Bearer <access_token>
   ↓
7. Backend validates token on each request
   ↓
8. If token expired (401), use refresh token
   ↓
9. Call POST /api/v1/auth/refresh with refresh_token
   ↓
10. Get new tokens and retry original request
```

### Workflow 2: Token Refresh

```
1. API call returns 401 Unauthorized
   ↓
2. Check if refresh_token exists
   ↓
3. Call POST /api/v1/auth/refresh
   ↓
4. If successful:
   - Store new tokens
   - Retry original request
   ↓
5. If refresh fails:
   - Clear stored tokens
   - Redirect to login page
```

### Workflow 3: User Logout

```
1. User clicks logout
   ↓
2. Call POST /api/v1/auth/logout with access_token
   ↓
3. Backend invalidates token
   ↓
4. Frontend clears stored tokens
   ↓
5. Redirect to login page
```

---

## Error Handling

### HTTP Status Codes

- **200 OK** - Request successful
- **400 Bad Request** - Invalid request payload
- **401 Unauthorized** - Missing or invalid authentication
- **404 Not Found** - Resource not found
- **500 Internal Server Error** - Server error

### Error Response Format

```json
{
  "error": "error_type",
  "message": "Detailed error message (optional)"
}
```

### Common Errors

#### Authentication Errors

**Invalid Credentials (401):**
```json
{
  "error": "invalid credentials"
}
```

**Invalid Token (401):**
```json
{
  "error": "unauthorized"
}
```

**Missing Token (400):**
```json
{
  "error": "missing token"
}
```

---

## Best Practices

### Security

1. **HTTPS in Production:** Always use HTTPS in production environments
2. **Secure Token Storage:** 
   - Use httpOnly cookies when possible
   - If using localStorage, be aware of XSS risks
   - Never log tokens
3. **Token Expiration:** Implement automatic token refresh before expiration
4. **Logout on Inactivity:** Clear tokens after period of inactivity
5. **CORS Configuration:** Configure CORS properly for your frontend domain

### Performance

1. **Token Caching:** Cache tokens to avoid unnecessary login calls
2. **Request Batching:** Batch multiple API calls when possible
3. **Error Retry Logic:** Implement exponential backoff for retries
4. **Connection Pooling:** Reuse HTTP connections

### Development

1. **Environment Variables:** Use environment variables for API URLs
2. **Error Logging:** Log API errors for debugging
3. **Request/Response Logging:** Log requests in development (not tokens!)
4. **API Versioning:** Always use versioned endpoints (/api/v1)

---

## Troubleshooting

### Issue: 401 Unauthorized on Protected Endpoints

**Causes:**
- Token not included in request
- Token expired
- Invalid token format

**Solutions:**
1. Verify Authorization header format: `Bearer <token>`
2. Check token hasn't expired (15 min default)
3. Try refreshing token
4. Re-login if refresh fails

### Issue: CORS Errors in Browser

**Cause:** API server not configured for your frontend domain

**Solution:**
Contact backend team to add your domain to CORS whitelist

### Issue: Token Refresh Loop

**Cause:** Refresh token also expired

**Solution:**
1. Clear all stored tokens
2. Redirect user to login page
3. User must re-authenticate

### Issue: Swagger UI Not Loading

**Causes:**
- API server not running
- Wrong URL
- Network issues

**Solutions:**
1. Verify API server is running
2. Check URL: http://localhost:8080/swagger/index.html
3. Check health endpoint: http://localhost:8080/healthz
4. Clear browser cache

---

## Developer Reference

### Build Strategy and Feature Flag

#### Overview

The Catalog API is a **feature-flagged component** that can be conditionally compiled into the `ai-services` binary. This provides flexibility for different deployment scenarios.

#### Default Behavior

**By default, the Catalog API is DISABLED** in regular builds:

```bash
# Default build (without Catalog API)
cd ai-services
make build
# Result: Binary does NOT include catalog commands
```

**Why disabled by default?**
1. **Release Focus:** GA releases focus on core AI services functionality
2. **Binary Size:** Keeps release binaries smaller and more focused
3. **Opt-in Development:** Developers explicitly enable when working on catalog features
4. **Separation of Concerns:** Catalog API is separate from core functionality and slated for the next release.

#### Enabling the Catalog API

**For Development:**
```bash
# Build with Catalog API enabled
cd ai-services
make build ENABLE_CATALOG_API=true

# Verify catalog command is available
./bin/ai-services catalog --help
```

**For Container Builds:**
```bash
# Build container image with Catalog API
cd ai-services
make image ENABLE_CATALOG_API=true
```

**For CI/CD:**
The GitHub Actions workflow automatically builds with the Catalog API enabled to ensure it compiles and works correctly:

```yaml
# CI builds both configurations
- Regular build (without catalog API)
- CI/CD build (with catalog API enabled)
```

#### Build Tag Implementation

The feature uses Go build tags for conditional compilation:

```go
//go:build catalog_api
```

### Regenerating API Documentation

If API endpoints change, developers need to regenerate Swagger docs:

```bash
cd ai-services
# Must enable catalog API to regenerate docs
make swagger ENABLE_CATALOG_API=true
make build ENABLE_CATALOG_API=true
```

**Important:** Swagger documentation generation requires the Catalog API to be enabled since it reads the API annotations from the catalog package.

### Running the API Server

**Build with Catalog API first:**
```bash
cd ai-services
make build ENABLE_CATALOG_API=true
```

**Then run the server:**
```bash
./bin/ai-services catalog apiserver \
  --admin-username admin \
  --admin-password-hash <bcrypt-hash> \
  --port 8080
```

### Generating Password Hash

```bash
cd ai-services
./bin/ai-services catalog hashpw
# Enter password when prompted
# Copy the generated hash
```

### Environment Variables

- `AUTH_JWT_SECRET` - Secret key for JWT signing (required for production)

### API Specification Files

- **JSON:** http://localhost:8080/swagger/doc.json
- **YAML:** `ai-services/docs/swagger.yaml`
- **Go Docs:** `ai-services/docs/docs.go`

### Adding New Endpoints

Developers should add Swagger annotations to new endpoints:

```go
// GetUser godoc
// @Summary Get user by ID
// @Description Get detailed information about a user
// @Tags Users
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} User
// @Failure 404 {object} ErrorResponse
// @Security BearerAuth
// @Router /users/{id} [get]
func (h *UserHandler) GetUser(c *gin.Context) {
    // implementation
}
```

Then regenerate docs with `make swagger`.

---

## Support and Resources

### Documentation
- **This Guide:** Complete API reference
- **Swagger UI:** Interactive API testing at http://localhost:8080/swagger/index.html
- **OpenAPI Spec:** Download from http://localhost:8080/swagger/doc.json

### Tools
- **Postman:** Import OpenAPI spec for testing
- **Insomnia:** Alternative API client
- **cURL:** Command-line testing

### Contact
For API issues or questions:
- Check this documentation first
- Test in Swagger UI
- Contact backend development team

---

**Document Version:** 1.0  
**API Version:** 1.0  
**Last Updated:** 2026-02-26  
**Auto Generated by AI Tool:** BOB