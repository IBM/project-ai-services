# Installation Guide

This guide walks you through installing and configuring AI-services on a RHEL environment.
Before you begin, confirm that all prerequisites are satisfied.

## Prerequisites

- User should have root privileges

- HMC-based access (UI and/or CLI)

- Spyre cards are attached, discoverable, and ACTIVATED (x cards)

- Podman is installed and running

- Hardware - 1 LPAR (16 CPUs (NUMA aligned), 768GB RAM, 600GB Storage) // check with the team once


## Installation

### Pull in the go binary

Download the latest `ai-services` binary from the [releases page](some link should live here).

Use the following `curl` command to download it (replace `<version>` with the desired release tag):

```bash
curl -LO https://example.com/ai-services/releases/download/<version>/<ai-services-binary>
sudo mv <ai-services-binary> /usr/local/bin/
sudo chmod +x <ai-services-binary>
```

### Run the binary to get started

```bash
% ai-services
A CLI tool for managing AI services infrastructure.

Usage:
  ai-services [command]

Available Commands:
  application Deploy and monitor the applications
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  version     Prints CLI version with more info

Flags:
  -h, --help      help for ai-services
  -v, --version   version for ai-services

Use "ai-services [command] --help" for more information about a command.
```

Note: The LPAR environment is configured and validated each time an application is created.

### Enable autocompletion feature (Optional) -> Shall be moved away from initial setup

Enable shell autocompletion for the CLI. Once activated, the CLI automatically suggests commands, flags, and arguments as you type, improving speed and reducing errors.

#### Enable for bash
```bash
% source < (ai-services completion bash) 
```

#### Enable for fish
```bash
% source < (ai-services completion fish) 
```

#### Enable for bash
```bash
% source < (ai-services completion bash) 
```

#### Enable for powershell
```bash
% source < (ai-services completion powershell) 
```
