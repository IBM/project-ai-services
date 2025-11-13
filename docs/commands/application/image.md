### Description

The image command allows you to pull new container images and list the images required by the application template.

### Avaiable Commands

| Command | Description |
|----------|----------|
| list | List container images for a given application template |
| pull | Pulls all container images for a given application template |

### Usage

```bash
ai-services application image [command] [flags]
```

### Example

```bash
ai-services application image list --template RAG
ai-services application image list  -t RAG 
ai-services application image pull  -t RAG 
```

### Flags

| Flag | Description |
|---------|----------|
|`-t, --template string` | Template name to use **(required)** |
|`-h, --help` | Show help message |
