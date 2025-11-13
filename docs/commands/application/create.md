### Description

The application command allows you to deploy new applications and monitor their status.
On successful execution of `create` command, the next steps are displayed in the terminal itself.

### Usage

```bash
ai-services application create [name] [flags]
```

### Example

```bash
ai-services application create rag-application-1 -t RAG --params UI_PORT=3000
```

### Flags

| Flag | Description |
|---------|----------|
|`-t, --template string` | Template name to use **(required)** |
|`--params strings` | Values Supported: UI_PORT=8000 |
|`--skip-model-download` | Set to true to skip model download during application creation |
|`--skip-validation strings` | Skip specific validation checks (comma-separated: root,rhel,rhn,power11,rhaiis) |
|`-h, --help` | Show help message |
|`--verbose` | Enable detailed logs. |

