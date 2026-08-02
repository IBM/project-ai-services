# Pull Request Naming Convention

## Format

```
type: Brief description
```

Or with ticket reference:

```
type: [TICKET-123] Brief description
```

## Types

| Type | When to Use | Examples |
|------|-------------|----------|
| **feat** | New features or capabilities | `feat: Add user authentication` |
| **fix** | Bug fixes | `fix: Resolve memory leak in cache` |
| **docs** | Documentation changes | `docs: Update installation guide` |
| **build** | Build, dependencies, or images | `build: Upgrade React to v18` |
| **chore** | Maintenance, cleanup | `chore: Remove unused imports` |
| **test** | Test additions or updates | `test: Add integration tests` |
| **perf** | Performance improvements | `perf: Optimize database queries` |
| **cicd** | CI/CD changes | `cicd: Add security scanning` |

## Examples

```
feat: Add user authentication
fix: Resolve memory leak in cache
docs: Update installation guide
build: Upgrade React to v18
chore: Remove unused imports
test: Add integration tests
perf: Optimize database queries
cicd: Add security scanning
fix: [AISERVICES-123] Resolve bug
feat: [Digitize-UI] Add export
```

## Rules

1. **Lowercase type** - `feat:` not `Feat:`
2. **Imperative mood** - "Add" not "Added" or "Adds"
3. **Space after colon** - `feat: ` not `feat:`
4. **Be specific** - "Fix login button alignment" not "Fix UI"
5. **Keep short** - 50-72 characters

## Validation

PRs are automatically validated via GitHub Actions. Invalid titles will fail the check.

**Regex pattern:**
```regex
^(feat|fix|docs|build|chore|test|perf|cicd):\s+.+$
```
