### Description

Similar to `podman ps` - lists containers deployed by AI-services. The command lists all the pods from all applications if application name is not provided.

### Usage

```bash
ai-services application ps
ai-services application ps [name] [flags]
```

### Example

```bash
ai-services application ps
ai-services application ps rag-application-1
```

### Flags

| Flag | Description |
|---------|----------|
|`-h, --help` | Show help message |
