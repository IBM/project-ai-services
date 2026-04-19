# Architecture-Service System

---

## Overview

This directory contains the design proposal for transforming the AI Services platform from a monolithic application structure to a flexible **architecture-service model**. The new system will enable modular, reusable services that can be deployed independently or as part of complete architectures.

---

## Documents

### 📄 [PROPOSAL.md](./PROPOSAL.md)
**Main design document** - Comprehensive proposal covering:
- Problem statement and current limitations
- Proposed solution and architecture
- Metadata schemas and directory structure
- User experience and deployment flows
- Technical details and algorithms
- Areas requiring work
- Success criteria and risks

### 📊 [Service-Dependencies-Diagram.md](./Service-Dependencies-Diagram.md)
**Visual diagrams** - Illustrates:
- Current state vs proposed state comparison
- Service dependency relationships
- Deployment layer structure
- Architecture composition
- Service discovery patterns

---

## Quick Reference

### What Problem Are We Solving?

**Current State:**
- Cannot deploy individual components (e.g., just chat)
- Services duplicated across multiple variants
- Manual configuration and tight coupling

**Proposed Solution:**
- RAG becomes an architecture (logical grouping)
- Services become independent, reusable components
- Automatic dependency resolution
- Deploy architectures OR individual services

### Key Concepts

| Concept | Description | Example |
|---------|-------------|---------|
| **Architecture** | Logical grouping of services | RAG = chat + digitize + summarize |
| **Deployable Service** | Independent deployable component | chat, digitize, summarize |
| **Dependency Service** | Service required by another service | chat depends on opensearch, embedding, instruct, reranker |
| **Auto-Detection** | System determines deployment mode | Checks architectures/ → services/ → applications/ |

### Deployment Examples

```bash
# Deploy full RAG architecture (all services)
ai-services application create my-rag --template rag

# Deploy individual service (NEW capability!)
ai-services application create my-chat --template chat

# Legacy compatibility
ai-services application create legacy --template rag --legacy
```

## Implementation Items

### 1: Foundation
- Define metadata schemas
- Create catalog system
- Implement dependency resolution

### 2: Service Extraction
- Extract services from RAG
- Create service templates
- Update service discovery

### 3: Deployment Logic
- Implement auto-detection for all application commands
- Create architecture deployer logic as a set of services
- Create service deployer to work in layers
- Add legacy support for existing template

### 4: CLI Updates
- Update application commands
- Update image/model commands
- Update info command

---

## Benefits

### For Users
- 🎯 **Seamless Deployment**: Single command for architectures or services
- 🔧 **Flexibility**: Deploy only what you need
- 🔄 **Automatic Dependencies**: No manual configuration
- ⚡ **Backward Compatible**: Existing commands work

### For Developers
- 🧩 **Modular Design**: Services are independent
- ♻️ **Reusability**: Share services across architectures
- 🛠️ **Maintainability**: Changes isolated to services
- 🚀 **Extensibility**: Easy to add new services

### For Operations
- 📊 **Resource Efficiency**: Deploy only needed services
- 🔍 **Clear Dependencies**: Explicit dependency graph
- 🎛️ **Service Discovery**: Convention-based naming
- 📈 **Scalability**: Independent service scaling

---

## Success Criteria

### Must Have
- ✅ Deploy full RAG architecture
- ✅ Deploy individual services
- ✅ Automatic dependency resolution
- ✅ Backward compatibility
- ✅ Service discovery without configuration
- ✅ Performance comparable to current system

### Nice to Have
- Optional service selection
- Reusing a running instance of a service
- Service health monitoring
- Rolling updates and replicas
- Performance metrics

---

## Questions?

For detailed information, see [PROPOSAL.md](./PROPOSAL.md).

For visual reference, see [Service-Dependencies-Diagram.md](./Service-Dependencies-Diagram.md).
