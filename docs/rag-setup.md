# Setting up RAG using AI-services

This section explains how to set up a RAG using the existing templates provided in AI-services.

## Pull and Run the AI-services binary 

To get started, follow the [installation guide](./installation.md) to pull and run the AI-services binary.

## Create an App Using the RAG Template

Initialize a new app using the built-in RAG template. It generates all essential resources needed to configure and run a RAG workflow.

```bash
$ ai-services application create <app-name> -t RAG --skip-validation numa --params UI_PORT=3000
```

## Place the Documents for 

Add your source documents to the designated ingestion directory path -> `/var/lib/ai-services/<app-name>/docs/`. These files will be processed and indexed for retrieval by the RAG pipeline.

## Start Document Ingestion

Trigger the ingestion process to parse and embed the uploaded documents. Once complete, the documents become searchable and ready for retrieval during chat interactions.

```bash
podman start <app-name>--ingest-docs-ingest-docs
```

## Access the Chatbot

Launch the chatbot interface connected to your RAG setup. By default, the bot runs on port 3000 and can be accessed at `http://<IP>:3000`.

