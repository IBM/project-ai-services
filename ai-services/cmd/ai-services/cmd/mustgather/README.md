# Must-Gather Command

The `must-gather` command collects comprehensive debugging information from AI Services deployments for troubleshooting purposes. It automatically sanitizes sensitive information like secrets, passwords, and tokens.

## Features

- **Multi-Runtime Support**: Works with both Podman and OpenShift runtimes
- **Comprehensive Data Collection**: Gathers pods, logs, events, services, deployments, and more
- **Automatic Secret Sanitization**: Redacts sensitive information from all collected data
- **Flexible Filtering**: Can collect data for specific applications or all applications
- **Organized Output**: Creates timestamped directories with well-structured data

## Usage

### Basic Usage

```bash
# Collect debugging information for Podman runtime
ai-services must-gather --runtime podman

# Collect debugging information for OpenShift runtime
ai-services must-gather --runtime openshift
```

### Advanced Usage

```bash
# Collect data for a specific application
ai-services must-gather --runtime podman --application rag

# Specify custom output directory
ai-services must-gather --runtime openshift --output-dir /tmp/debug-info

# Combine options
ai-services must-gather --runtime podman --application rag-cpu --output-dir ./debug
```

## Command Options

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--runtime` | | Runtime to use (podman or openshift) | Required |
| `--application` | `-a` | Specific application name to gather data for | All applications |
| `--output-dir` | `-o` | Base directory to save collected information (creates must-gather.local.<id> subdirectory) | `.` (current directory) |

## Collected Information

### Podman Runtime

- **System Information**
  - Podman version
  - Podman system info
  - System disk usage

- **Pod Information**
  - Pod list and detailed inspection
  - Pod specifications (sanitized)

- **Container Information**
  - Container list and detailed inspection
  - Container specifications (sanitized)
  - Container logs (last 1000 lines, sanitized)

- **Network Information**
  - Network configurations

- **Volume Information**
  - Volume list and details

### OpenShift Runtime

- **Cluster Information**
  - Node information
  - Namespace list

- **Namespace Information**
  - Namespace details and configurations

- **Pod Information**
  - Pod specifications (sanitized)
  - Container logs (last 1000 lines, sanitized)
  - Environment variables (sanitized)

- **Workload Information**
  - Deployments
  - Services
  - ConfigMaps (sanitized)
  - Routes

- **Events**
  - Namespace events

## Secret Sanitization

The must-gather command automatically sanitizes sensitive information by detecting and redacting:

- Passwords and credentials
- API keys and tokens
- Secrets and private keys
- Certificates
- OAuth tokens
- Bearer tokens
- Any field matching common secret patterns

Sanitized values are replaced with `***REDACTED***`.

### Sanitization Patterns

The following key patterns are automatically detected and sanitized:
- `password`, `passwd`, `pwd`
- `secret`, `token`, `key`, `apikey`, `api_key`
- `credential`, `auth`, `authorization`
- `private`, `cert`, `certificate`
- `access_token`, `refresh_token`
- `bearer`, `oauth`

## Output Structure

The command creates a timestamped directory with the following structure:

### Podman Output Structure
```
must-gather-output/
└── must-gather.local.<numeric_id>/
    ├── podman-version.txt
    ├── podman-info.json
    ├── system-df.txt
    ├── pods/
    │   └── <pod-name>-inspect.json
    ├── containers/
    │   └── <container-name>-inspect.json
    ├── logs/
    │   └── <container-name>.log
    ├── network/
    │   └── networks.json
    └── volumes/
        └── volumes.json
```

### OpenShift Output Structure
```
must-gather-output/
└── must-gather.local.<numeric_id>/
    ├── cluster/
    │   ├── nodes.txt
    │   └── namespaces.txt
    ├── namespace/
    │   └── <namespace>.txt
    ├── pods/
    │   └── <pod-name>/
    │       ├── spec.txt
    │       ├── env-vars.txt
    │       └── <container-name>.log
    ├── events/
    │   └── events.txt
    ├── services/
    │   └── <service-name>.txt
    ├── deployments/
    │   └── <deployment-name>.txt
    ├── configmaps/
    │   └── <configmap-name>.txt
    └── routes/
        └── routes.txt
```

## Examples

### Example 1: Debug a Failing RAG Application on Podman

```bash
ai-services must-gather --runtime podman --application rag --output-dir ./rag-debug
```

This collects all information related to the RAG application and saves it to `./rag-debug/must-gather.local.<numeric_id>/`.

### Example 2: Collect Full Cluster Information on OpenShift

```bash
ai-services must-gather --runtime openshift --output-dir /tmp/cluster-debug
```

This collects comprehensive cluster information including all applications and saves it to `/tmp/cluster-debug/must-gather.local.<numeric_id>/`.

### Example 3: Quick Debug with Default Settings

```bash
ai-services must-gather --runtime podman
```

This collects all information and saves it to the default location `./must-gather.local.<numeric_id>/`.

## Troubleshooting

### Permission Issues

If you encounter permission errors:

**Podman:**
```bash
# Ensure your user has access to podman
sudo usermod -aG podman $USER
# Re-login to apply group changes
```

**OpenShift:**
```bash
# Ensure you're logged in to the cluster
oc login <cluster-url>
# Verify you have the necessary permissions
oc auth can-i get pods --all-namespaces
```

### Missing Data

If some data is missing from the output:
- Check the command output for warning messages
- Verify you have the necessary permissions
- Ensure the resources exist in the specified namespace/application

### Large Output Size

If the output directory is too large:
- Use the `--application` flag to filter specific applications
- The command automatically limits logs to the last 1000 lines per container
- Consider cleaning up old must-gather outputs regularly

## Best Practices

1. **Regular Collection**: Run must-gather periodically to track system state over time
2. **Before Reporting Issues**: Always collect must-gather data before reporting issues
3. **Secure Storage**: Store must-gather outputs securely as they may contain system information
4. **Clean Up**: Regularly clean up old must-gather outputs to save disk space
5. **Specific Filtering**: Use the `--application` flag when debugging specific applications

## Integration with Support

When reporting issues to support:

1. Run the must-gather command
2. Compress the output directory:
   ```bash
   tar -czf must-gather-$(date +%Y%m%d-%H%M%S).tar.gz must-gather-output/
   ```
3. Attach the compressed file to your support ticket
4. Include the command you used to generate the must-gather

## Security Considerations

- The must-gather command automatically sanitizes secrets, but always review the output before sharing
- Avoid running must-gather with elevated privileges unless necessary
- Store must-gather outputs in secure locations
- Delete must-gather outputs after they're no longer needed
- Be cautious when sharing must-gather data as it contains system configuration information

## Contributing

To add new data collection capabilities:

1. Add collection methods to the appropriate gatherer (`podman.go` or `openshift.go`)
2. Ensure all collected data is sanitized using the `SecretSanitizer`
3. Update this README with the new data being collected
4. Add tests for the new functionality