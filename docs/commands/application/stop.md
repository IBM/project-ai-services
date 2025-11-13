### Description

Stops all or specific pods in the application

### Usage

```bash
ai-services application stop [name] [flags]
```

### Example

```bash
ai-services application stop rag-application-1
ai-services application stop rag-application-1 --pod rag-application-1--pod-name
```

### Flags

| Flag | Description |
|---------|----------|
|`--pod string`| Specific pod name(s) to stop (optional) |
|`--skip-logs`| Skip displaying logs after starting the pod |
|`-h, --help`| Show help message |