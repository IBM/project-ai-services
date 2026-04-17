# Application Deployment API Proposal

**Version:** 1.0
**Date:** April 17, 2026
**Status:** Draft
**Author:** Based on Application-Deployment-API-Design.docx

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Background and Motivation](#2-background-and-motivation)
   - 2.1 [Current State](#21-current-state)
   - 2.2 [Problem Statement](#22-problem-statement)
   - 2.3 [Goals](#23-goals)
3. [Architecture Overview](#3-architecture-overview)
   - 3.1 [Key Concepts](#31-key-concepts)
   - 3.2 [System Components](#32-system-components)
4. [API Specification](#4-api-specification)
   - 4.1 [Base URL](#41-base-url)
   - 4.2 [Authentication](#42-authentication)
   - 4.3 [Endpoint Categories](#43-endpoint-categories)
5. [Security Considerations](#5-security-considerations)
   - 5.1 [Authentication](#51-authentication)
   - 5.2 [Authorization](#52-authorization)
   - 5.3 [API Security](#53-api-security)
   - 5.4 [Secrets Management](#54-secrets-management)
6. [Error Handling](#6-error-handling)
   - 6.1 [Error Response Format](#61-error-response-format)
   - 6.2 [HTTP Status Codes](#62-http-status-codes)
7. [API Usage Examples](#7-api-usage-examples)

## 1. Executive Summary

This proposal outlines the design and implementation of a comprehensive REST API for managing application deployments in the AI Services Catalog. The API will enable users to deploy, monitor, and manage AI service applications through a unified interface, supporting both individual services and complete architectures across multiple runtime environments (Podman and OpenShift).

## 2. Background and Motivation

### 2.1 Current State
The AI Services Catalog currently provides various AI services (chat, summarization, digitization) that can be deployed independently. However, there is no unified API for managing these deployments programmatically.

### 2.2 Problem Statement
Users need a standardized way to:
- Deploy AI services individually or as complete architectures
- Monitor deployment status and health
- Manage service configurations
- Access service endpoints
- Handle authentication and authorization

### 2.3 Goals
1. Provide a RESTful API for application lifecycle management
2. Support both architecture-level (multiple services) and service-level deployments
3. Enable multi-runtime support (Podman and OpenShift)
4. Implement secure authentication and authorization


## 3. Architecture Overview

### 3.1 Key Concepts

**Architecture**: A collection of multiple services that work together as a cohesive application (e.g., RAG architecture includes chat, summarization, and digitization services).

**Service**: An individual AI service that can be deployed standalone (e.g., summarization service, chat service).

**Runtime**: The deployment environment (Podman for local/development, OpenShift for production/cluster).

### 3.2 System Components

```
┌─────────────────────────────────────────────────────────────┐
│                        API Gateway                           │
│                   (http://localhost:8080)                    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Authentication Layer                      │
│                  (JWT Bearer Token Auth)                     │
└─────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌──────────────┐    ┌──────────────────┐    ┌──────────────┐
│ Application  │    │    Catalog       │    │     Auth     │
│ Management   │    │   Management     │    │  Management  │
└──────────────┘    └──────────────────┘    └──────────────┘
        │                     │
        ▼                     ▼
┌──────────────────────────────────────────┐
│         Runtime Orchestrators             │
│    ┌──────────┐      ┌──────────┐       │
│    │  Podman  │      │ OpenShift│       │
│    └──────────┘      └──────────┘       │
└──────────────────────────────────────────┘
```

## 4. API Specification

### 4.1 Base URL
```
http://localhost:8080/api/v1
```

### 4.2 Authentication

All endpoints (except `/auth/*`) require JWT Bearer token authentication:
```
Authorization: Bearer <access_token>
```

**Token Lifecycle:**
- Access tokens expire after 15 minutes
- Refresh tokens valid for 7 days
- Token blacklisting on logout

### 4.3 Endpoint Categories

#### Authentication Endpoints
- `POST /api/v1/auth/login` - User login
- `POST /api/v1/auth/refresh` - Refresh access token
- `POST /api/v1/auth/logout` - User logout
- `GET /api/v1/auth/me` - Get current user info

#### Application Management Endpoints
- `GET /api/v1/applications` - List all deployments
- `GET /api/v1/applications/{id}` - Get deployment details
- `POST /api/v1/applications` - Create new deployment
- `PUT /api/v1/applications/{id}` - Update deployment
- `DELETE /api/v1/applications/{id}` - Delete deployment
- `GET /api/v1/applications/{id}/ps` - Get pod/container health status

#### Catalog Endpoints
- `GET /api/v1/architectures` - List available architectures
- `GET /api/v1/architectures?name="Digital Assistant"` - Get architecture details

- `GET /api/v1/services` - List available services
- `GET /api/v1/services?name="Summarization"` - Get service details
- `GET /api/v1/services/params?name="Summarization"` - Get service custom params


## 6. Error Handling

### 6.1 Error Response Format
```json
{
  "error": "error_code",
  "message": "Human-readable error message",
  "details": [
    {
      "field": "field_name",
      "message": "Field-specific error"
    }
  ]
}
```

### 6.2 HTTP Status Codes
- `200 OK` - Successful request
- `202 Accepted` - Async operation initiated
- `400 Bad Request` - Invalid request
- `401 Unauthorized` - Authentication required
- `404 Not Found` - Resource not found
- `409 Conflict` - Resource conflict (e.g., duplicate name)
- `422 Unprocessable Entity` - Validation failed
- `500 Internal Server Error` - Server error

## 7. API Usage Examples

### Example 1: Deploy RAG Architecture
```bash
# 1. Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"password"}'

# Response: {"access_token": "eyJhbGc...", ...}

# 2. List Available Architectures
curl -X GET http://localhost:8080/api/v1/architectures \
  -H "Authorization: Bearer <token>"

# 3. Deploy RAG Architecture
curl -X POST http://localhost:8080/api/v1/applications \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "rag-prod",
    "type": "architecture",
    "template": "rag",
    "runtime": "openshift",
    "configuration": {
      "model": "granite-7b-instruct",
      "replicas": 2
    }
  }'

# Response: {"id": "app-123...", "status": "downloading", ...}

# 4. Check Deployment Status
curl -X GET http://localhost:8080/api/v1/applications/app-123... \
  -H "Authorization: Bearer <token>"
```

### Example 2: Deploy Single Service
```bash
# 1. List Available Services
curl -X GET http://localhost:8080/api/v1/services \
  -H "Authorization: Bearer <token>"

# 2. Deploy Summarization Service
curl -X POST http://localhost:8080/api/v1/applications \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "summarization-dev",
    "type": "service",
    "template": "summarization",
    "runtime": "podman",
    "configuration": {
      "model": "granite-7b-instruct"
    }
  }'
```

---
