# AI-Services

AI services, part of the IBM Open-Source AI Foundation for Power, deliver pre-built AI capabilities and integration with inferencing solutions like Red Hat AI Inference Server. Optimized for IBM Spyre™ on Power, they enable fast deployment and support models such as LLMs, embeddings, and re-rankers—helping enterprises scale AI efficiently.

## 📘 Table of Contents

- [**🌟 Introduction**](https://www.ibm.com/docs/aiservices?topic=services-introduction)

- [**🛠️ Installation Guide**](https://www.ibm.com/docs/aiservices?topic=services-installation)

- **💻 CLI Reference**
  - [Auto-completion](https://www.ibm.com/docs/aiservices?topic=reference-auto-completion)
  - [Commands](https://www.ibm.com/docs/aiservices?topic=reference-commands)

- **🎓 Tutorials**
  - [Installing in air-gapped environment](https://www.ibm.com/docs/aiservices?topic=tutorials-installing-in-air-gapped-environment)
  - [Setting up RAG Chatbot](https://www.ibm.com/docs/aiservices?topic=tutorials-setting-up-rag-chatbot)

- [**⚠️ Limitations**](https://www.ibm.com/docs/aiservices?topic=services-limitations)

- [**🧯 Troubleshooting**](https://www.ibm.com/docs/aiservices?topic=services-troubleshooting)

- [**❓ FAQ**](https://www.ibm.com/docs/aiservices?topic=services-faq)

---

## Quick Start

### Pull in AI-services binary

Download the latest ai-services binary from the [releases page](https://github.com/IBM/project-ai-services/releases). Use the following curl command to download it (replace `version` with the desired release tag):

```bash
$ curl -LO https://github.com/IBM/project-ai-services/releases/download/<version>/ai-services
$ sudo chmod +x ai-services
$ sudo mv ai-services /usr/local/bin/
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
