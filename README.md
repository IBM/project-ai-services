# AI-Services

AI Services, part of the IBM Open-Source AI Foundation for Power, deliver pre-built AI capabilities and integration with inferencing solutions like Red Hat AI Inference Server. Optimized for IBM Spyre™ on Power, they enable fast deployment and support models such as LLMs, embeddings, and re-rankers—helping enterprises scale AI efficiently.

## 📘 Table of Contents

- [**🗃️ Repository Structure**](#repository-structure)

- [**🌟 Introduction**](https://www.ibm.com/docs/aiservices?topic=services-introduction)

- [**🛠️ Configuration and Installation**](https://www.ibm.com/docs/aiservices?topic=services-configuration-installation)
  - [Prerequisites](https://www.ibm.com/docs/aiservices?topic=installation-prerequisites)
  - [Quick Start](#quick-start)
  - [Set-up AI Services](https://www.ibm.com/docs/aiservices?topic=installation-setup-ai-services)
  - [Configuring in Air-gapped Environment](https://www.ibm.com/docs/aiservices?topic=installation-configuring-air-gapped-environment)

- [**🤖 RAG Chatbot**](https://www.ibm.com/docs/en/aiservices?topic=services-rag-chatbot)
  - [Architecture](https://www.ibm.com/docs/en/aiservices?topic=chatbot-architecture)
  - [Setting up RAG Chatbot](https://www.ibm.com/docs/en/aiservices?topic=chatbot-setting-up-rag)

- **💻 CLI Reference**
  - [Auto-completion](https://www.ibm.com/docs/aiservices?topic=reference-auto-completion)
  - [Commands](https://www.ibm.com/docs/aiservices?topic=reference-commands)

- [**🧯 Troubleshooting**](https://www.ibm.com/docs/aiservices?topic=services-troubleshooting)

- [**❓ FAQ**](https://www.ibm.com/docs/aiservices?topic=services-faq)

- [**⚠️ Limitations**](https://www.ibm.com/docs/aiservices?topic=services-limitations)

- [**👩‍💻 Developer Guide**](https://www.ibm.com/docs/en/aiservices?topic=services-developer-guide)

- [**🚀 Contributing**](./CONTRIBUTING.md)

---

## Quick Start

### Pull in AI Services binary

Download the latest ai-services binary from the [releases page](https://github.com/IBM/project-ai-services/releases). Use the following curl command to download it (replace `version` with the desired release tag):

```bash
$ curl -LO https://github.com/IBM/project-ai-services/releases/download/<version>/ai-services
$ sudo chmod +x ai-services
$ sudo mv ai-services /usr/local/bin/
```

### Run the binary to get started

```bash
% ai-services --help
A CLI tool for managing AI Services infrastructure.

Usage:
  ai-services [command]

Available Commands:
  application   Deploy and monitor the applications
  completion    Generate the autocompletion script for the specified shell
  help          Help about any command
  version       Prints CLI version with more info

Flags:
  -h, --help      help for ai-services
  -v, --version   version for ai-services

Use "ai-services [command] --help" for more information about a command.
```

---

## Repository Structure

```bash
project-ai-services/
├── README.md          # Project documentation
├── ai-services/       # CLI tool for project-ai-services
│   ├── assets/        # Application template files
├── images/            # Helper/Utility image assets
├── spyre-rag/         # Spyre RAG implementation
├── test/              # Test assets
│   ├── golden/        # Golden dataset
```
