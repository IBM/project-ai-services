### Description

Displays the logs from application pods and containers.

### Usage

```bash
ai-services application logs [flags]
```

### Example

```bash
ai-services application logs --pod temp--pod3
ai-services application logs --pod temp--pod3 --container 4166daebd7a0
```

### Flags

| Flag | Description |
|---------|----------|
|`--container string`| Container logs to show logs from (Optional) |
|`-h, --help` | Show help message |
|`--pod string `|Pod name to show logs from **(required)** |
