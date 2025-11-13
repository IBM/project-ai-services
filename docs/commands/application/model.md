## Subcommand: model

### Description

The model command allows you to download new models and list the models required by the application template.

### Avaiable Commands

| Command | Description |
|----------|----------|
| download | Download models for a given application template |
| list | List models for a given application template |

### Usage

```bash
ai-services application model [command] [flags]
```

### Example

```bash
ai-services application model list --template RAG
ai-services application model list  -t RAG 
ai-services application model download  -t RAG 
```

### Flags

| Flag | Description |
|---------|----------|
|`-t, --template string` | Template name to use **(required)** |
|`-h, --help` | Show help message |