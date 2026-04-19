# Architecture-Service System Design Proposal

---

## Executive Summary

This proposal outlines a transformation of the AI Services platform from a monolithic application structure to a flexible **architecture-service model**. The new system will enable modular, reusable services that can be deployed independently or as part of complete architectures, with automatic dependency resolution and full backward compatibility.

### Vision

Enable users to:
- Deploy complete AI architectures (e.g., RAG) with a single command
- Deploy individual services (e.g., chat, digitize) with automatic dependency handling
- Reuse services across multiple architectures
- Maintain backward compatibility with existing deployments

### Key Benefits

- 🎯 **Seamless Deployment**: Single command deploys architectures or services
- 🔧 **Modular Design**: Services are independent, reusable components
- 🔄 **Automatic Dependencies**: System resolves and deploys dependencies automatically
- 🤖 **Service Discovery**: Convention-based naming eliminates manual configuration
- ⚡ **Backward Compatible**: Existing commands continue to work
- 🚀 **Future-Ready**: Foundation for advanced orchestration features

---

## Problem Statement

### Current State: Monolithic Applications

Today's RAG application is deployed as a monolithic unit where:

**Limitations:**
1. **Inflexibility**: Cannot deploy individual components (e.g., just chat or just digitize)
2. **Code Duplication**: Same pods (opensearch, vllm-server) duplicated across rag, rag-cpu, rag-dev
3. **Tight Coupling**: Changes to one component affect the entire application
4. **Maintenance Burden**: Updates require modifying multiple application definitions
5. **Limited Reusability**: Cannot share services across different architectures
6. **Monolithic Model Server**: vllm-server bundles 3 models in one pod, preventing independent scaling

### Example: Current RAG Deployment

```
assets/applications/rag/
├── metadata.yaml (monolithic definition)
└── podman/
    ├── metadata.yaml
    ├── values.yaml
    └── templates/
        ├── chat-bot.yaml.tmpl
        ├── digitize.yaml.tmpl
        ├── opensearch.yaml.tmpl
        ├── vllm-server.yaml.tmpl
        └── summarize-api.yaml.tmpl
```

**Problems:**
- All components bundled together
- opensearch, vllm-server duplicated in rag-cpu, rag-dev
- Cannot deploy just chat without modifying templates
- Service URLs hardcoded (e.g., `opensearch:9200`, `vllm-server:8000`)
- vllm-server combines 3 models (instruct, embedding, reranker) in one pod
- Cannot scale models independently (e.g., scale instruct without embedding)

---

## Proposed Solution: Architecture-Service Model

### Core Concepts

#### 1. Architecture
A **logical grouping** of services that work together to provide a complete solution.

**Characteristics:**
- Defines required and optional services
- No deployment templates (uses service templates)
- Specifies service versions and constraints
- Can be deployed as a unit

**Example: RAG Architecture**
```yaml
id: rag
name: "Digital Assistant"
services:
  - id: chat
    version: ">=1.0.0"
  - id: digitize
    version: ">=1.0.0"
  - id: summarize
    version: ">=1.0.0"
    optional: true
```

#### 2. Service
An **independent, deployable component** with its own lifecycle.

**Characteristics:**
- Self-contained with dependencies declared
- Reusable across multiple architectures
- Runtime-specific configurations (Podman, OpenShift)
- Own deployment templates and values

**Example: Chat Service**
```yaml
id: chat
name: "Question and Answer"
dependencies:
  - id: opensearch
    version: ">=1.0.0"
  - id: instruct
    version: ">=1.0.0"
  - id: embedding
    version: ">=1.0.0"
  - id: reranker
    version: ">=1.0.0"
```

#### 3. Service Types

**Deployable Services** (User-Facing):
- Can be deployed individually
- Have their own UI/API endpoints
- Examples: chat, digitize, summarize

**Infrastructure Services** (Dependencies):
- Deployed automatically when needed
- Shared across multiple services
- Examples: opensearch, instruct, embedding, reranker

---

## Proposed Architecture Design

### Directory Structure

```
assets/
├── architectures/              # NEW: Architecture definitions
│   └── rag/
│       └── metadata.yaml       # Lists services, no templates
│
├── services/                   # NEW: Reusable service definitions
│   ├── chat/                   # Deployable service
│   │   ├── metadata.yaml       # Service info + dependencies
│   │   ├── podman/
│   │   │   ├── metadata.yaml   # Runtime config
│   │   │   ├── values.yaml     # Default values
│   │   │   ├── steps/          # Post-deployment guidance
│   │   │   │   ├── info.md
│   │   │   │   ├── next.md
│   │   │   │   └── vars_file.yaml
│   │   │   └── templates/      # Pod templates
│   │   │       └── chat-bot.yaml.tmpl
│   │   └── openshift/
│   │       └── ...
│   │
│   ├── digitize/               # Deployable service
│   ├── summarize/              # Deployable service
│   ├── opensearch/             # Infrastructure service
│   ├── instruct/               # Infrastructure service (AI model)
│   ├── embedding/              # Infrastructure service (AI model)
│   └── reranker/               # Infrastructure service (AI model)
│
└── applications/               # LEGACY: Existing monolithic apps
    └── rag/                    # Kept for backward compatibility
```

### Metadata Schemas

#### Architecture Metadata

```yaml
# assets/architectures/rag/metadata.yaml
id: rag
name: "Digital Assistant"
description: "RAG architecture with Q&A, digitize, and summarize"
version: "1.0.0"
type: architecture

certified_by: "IBM"
supported_runtimes:
  - podman
  - openshift

services:
  - id: chat
    version: ">=1.0.0"
  - id: digitize
    version: ">=1.0.0"
  - id: summarize
    version: ">=1.0.0"
    optional: true
```

#### Service Metadata

```yaml
# assets/services/chat/metadata.yaml
id: chat
name: "Question and Answer"
description: "Answer questions using RAG"
version: "1.0.0"
type: service

certified_by: "IBM"

architectures:
  - rag

dependencies:
  - id: opensearch
    version: ">=1.0.0"
  - id: instruct
    version: ">=1.0.0"
  - id: embedding
    version: ">=1.0.0"
  - id: reranker
    version: ">=1.0.0"
```

#### Runtime Metadata

```yaml
# assets/services/chat/podman/metadata.yaml
name: chat
version: "1.0.0"
runtime: podman

# can have runtime specific details in future eg: resource requirements.
```

---

## Proposed User Experience

### Scenario 1: Deploy Full RAG Architecture

```bash
# User command (same as today!)
ai-services application create my-rag --template rag
```

**What happens:**
1. System detects `rag` is an architecture
2. Loads architecture metadata
3. Resolves all services (chat, digitize, summarize)
4. Resolves dependencies (opensearch, instruct, embedding, reranker)
5. Calculates deployment order (topological sort)
6. Deploys services layer by layer
7. Prints next steps

**Result:**
- All services deployed: chat, digitize, summarize
- All dependencies deployed: opensearch, instruct, embedding, reranker
- Services can discover each other via naming convention

### Scenario 2: Deploy Individual Service

```bash
# NEW capability!
ai-services application create my-chat --template chat
```

**What happens:**
1. System detects `chat` is a service
2. Loads service metadata
3. Resolves dependencies (opensearch, instruct, embedding, reranker)
4. Calculates deployment order
5. Deploys dependencies first, then chat
6. Prints next steps

**Result:**
- Chat service deployed
- Only required dependencies deployed (not digitize or summarize)
- Efficient resource usage

### Scenario 3: Legacy Compatibility

```bash
# Existing deployments continue to work
ai-services application create legacy-app --template rag --legacy
```

**What happens:**
1. `--legacy` flag forces old behavior
2. Uses monolithic templates from `assets/applications/rag/`
3. Deploys as before

**Result:**
- Backward compatibility maintained
- No breaking changes for existing users

---

## Service Discovery Pattern

### Convention-Based Naming

Services discover each other using a predictable naming pattern:

```
{{ .AppName }}--<service-id>:<port>
```

**Example:**
```
Application: production-rag
Service: opensearch
Port: 9200
URL: production-rag--opensearch:9200
```

**Benefits:**
- No manual configuration needed
- Works across all services
- Predictable and debuggable
- Supports multiple deployments

### Template Usage

```yaml
# In chat service template
env:
  - name: OPENSEARCH_URL
    value: "{{ .AppName }}--opensearch:9200"
  - name: INSTRUCT_URL
    value: "{{ .AppName }}--instruct:8000"
  - name: EMBEDDING_URL
    value: "{{ .AppName }}--embedding:8000"
  - name: RERANKER_URL
    value: "{{ .AppName }}--reranker:8000"
```

---

## Technical Implementation Details

### Catalog System

**Package:** `internal/pkg/catalog/`

**Functions:**
```go
// Load architecture metadata
func LoadArchitecture(id string) (*Architecture, error)

// Load service metadata
func LoadService(id string) (*Service, error)

// Load runtime metadata
func LoadServiceRuntimeMetadata(serviceID, runtime string) (*RuntimeMetadata, error)

// Resolve dependencies recursively
func ResolveServiceDependencies(serviceIDs ...string) ([]string, error)

// Calculate deployment order (topological sort)
func GetDeploymentOrder(serviceIDs []string) ([][]string, error)

// Validate dependencies (no cycles, all exist)
func ValidateDependencies(serviceIDs []string) error
```

### Deployment Flow

```
1. Parse user command
   ├─> Extract template name
   └─> Check for --legacy flag

2. Auto-detect deployment mode
   ├─> Check assets/architectures/<template>/
   ├─> Check assets/services/<template>/
   └─> Check assets/applications/<template>/

3. Load metadata
   ├─> Architecture: Load architecture + all services
   ├─> Service: Load service + dependencies
   └─> Legacy: Load application metadata

4. Resolve dependencies
   ├─> Build dependency graph
   ├─> Detect circular dependencies
   └─> Calculate deployment order (layers)

5. Validate
   ├─> Check all services exist
   ├─> Verify version compatibility
   └─> Detect circular dependencies

6. Deploy layer by layer
   ├─> Layer 1: Services with no dependencies
   ├─> Layer 2: Services depending on Layer 1
   └─> Layer N: Services depending on previous layers

7. Post-deployment
   ├─> Print service status
   ├─> Show next steps
   └─> Display service URLs
```

### Dependency Resolution Algorithm

**Topological Sort (Kahn's Algorithm):**

```
1. Build dependency graph
   - Nodes: Services
   - Edges: Dependencies

2. Calculate in-degree for each node
   - In-degree = number of dependencies

3. Initialize queue with nodes having in-degree 0
   - These are services with no dependencies

4. Process queue:
   - Remove node from queue (add to current layer)
   - Decrease in-degree of dependent nodes
   - Add nodes with in-degree 0 to next layer

5. Repeat until all nodes processed

6. If nodes remain, circular dependency exists
```

**Example:**
```
Services: chat, opensearch, instruct, embedding, reranker

Dependencies:
- chat → opensearch, instruct, embedding, reranker
- instruct → (no dependencies)
- embedding → (no dependencies)
- reranker → (no dependencies)
- opensearch → (no dependencies)

Result:
Layer 1: [opensearch, instruct, embedding, reranker]
Layer 2: [chat]
```

---

## Areas Requiring Work

### 1. Metadata Management
- [ ] Define and validate metadata schemas
- [ ] Create schema validation logic
- [ ] Implement version compatibility checks
- [ ] Add metadata migration tools

### 2. Catalog System
- [ ] Implement catalog loader
- [ ] Add caching for performance
- [ ] Create metadata indexing
- [ ] Add search capabilities

### 3. Dependency Resolution
- [ ] Implement graph builder
- [ ] Add topological sorting
- [ ] Create circular dependency detection
- [ ] Add version constraint resolution

### 4. Deployment Orchestration
- [ ] Implement auto-detection logic
- [ ] Create architecture deployer
- [ ] Create service deployer
- [ ] Add parallel deployment within layers
- [ ] Implement readiness checks

### 5. Service Discovery
- [ ] Implement naming convention
- [ ] Update all templates
- [ ] Add service registry (future)
- [ ] Create discovery documentation

### 6. CLI Updates
- [ ] Update application create command
- [ ] Add --legacy flag
- [ ] Update image commands
- [ ] Update model commands
- [ ] Add service listing commands

### 7. Template Migration
- [ ] Extract services from RAG
- [ ] Extract services from RAG-CPU
- [ ] Extract services from RAG-DEV
- [ ] Update service discovery URLs
- [ ] Create step files for each service

### 8. Testing
- [ ] Unit tests for catalog
- [ ] Unit tests for resolver
- [ ] Integration tests for deployment
- [ ] End-to-end tests
- [ ] Performance tests

### 9. Documentation
- [ ] User guide
- [ ] Developer guide
- [ ] Migration guide
- [ ] API documentation
- [ ] Architecture diagrams

### 10. Backward Compatibility
- [ ] Maintain legacy code path
- [ ] Add compatibility tests
- [ ] Create migration tools
- [ ] Document breaking changes (if any)

---

## Success Criteria

### Functional Requirements
- ✅ Deploy full RAG architecture with single command
- ✅ Deploy individual services (chat, digitize, summarize)
- ✅ Automatic dependency resolution
- ✅ Service discovery works without configuration
- ✅ Backward compatibility with existing deployments
- ✅ Support for both Podman and OpenShift runtimes

### Non-Functional Requirements
- ✅ Deployment time comparable to current system
- ✅ Clear error messages for dependency issues
- ✅ Comprehensive test coverage
- ✅ Complete documentation
- ✅ No breaking changes for existing users

### User Experience
- ✅ Simple commands (no complex flags for common cases)
- ✅ Clear feedback during deployment
- ✅ Helpful next steps after deployment
- ✅ Easy troubleshooting

---

## Risks and Mitigation

### Risk 1: Complexity
**Risk:** System becomes too complex to maintain  
**Mitigation:**
- Keep metadata schemas simple
- Use standard algorithms (topological sort)
- Comprehensive documentation
- Code reviews

### Risk 2: Performance
**Risk:** Dependency resolution slows down deployment  
**Mitigation:**
- Cache metadata
- Optimize graph algorithms
- Parallel deployment within layers
- Performance testing

### Risk 3: Breaking Changes
**Risk:** Existing deployments break  
**Mitigation:**
- Maintain legacy code path
- Add --legacy flag
- Comprehensive compatibility testing
- Clear migration guide

### Risk 4: Service Extraction
**Risk:** Incorrect service boundaries  
**Mitigation:**
- Careful analysis of current system
- Prototype with one service first
- Iterative refinement
- Team review

---

## Future Enhancements

- Optional service selection
- Service health monitoring
- Rolling updates
- Service scaling
- Cross-architecture dependencies
- User provided services
- Advanced orchestration

---

## Conclusion

The architecture-service system will transform the AI Services platform from a monolithic structure to a flexible, modular system. This design enables:

- **Seamless deployment** of complete architectures or individual services
- **Automatic dependency management** eliminating manual configuration
- **Service reusability** across multiple architectures
- **Backward compatibility** ensuring no disruption to existing users
- **Future extensibility** providing foundation for advanced features

---

## Appendix

### A. Glossary

- **Architecture**: Logical grouping of services
- **Service**: Independent deployable component
- **Dependency**: Service required by another service
- **Topological Sort**: Algorithm for ordering dependencies
- **Service Discovery**: Mechanism for services to find each other
- **Legacy Mode**: Backward-compatible deployment using old templates

