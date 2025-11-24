# Setting up RAG using AI-services

This section explains how to set up a RAG using the existing templates provided in AI-services.

![rag-setup](./assets/rag-tutorial.drawio.png)

##### Fig 1.0 - RAG Chatbot Setup

## Pull and Run the AI-services binary 

To get started, follow the [installation guide](./installation.md) to pull and run the AI-services binary.

## Create an App Using the RAG Template

Initialize a new application using the built-in template - RAG. It generates all essential resources required to configure and run a complete RAG workflow.
You can also specify UI port for the chatbot using the --params flag (for example: --params ui.port=3000). If not provided, the system automatically assigns a free port for the chatbot UI.

Provide a unique name for the application to ensure smooth deployment (replace with `<app-name>`).

```bash
$ ai-services application create <app-name> -t RAG --params ui.port=3000
```

**Replace 3000 with any port number you wish to use for rendering the UI.**

After the `create` command completes successfully, the next steps will appear in the output. Alternatively, you can follow the instructions below. Make sure to replace `<app-name>` with your application name accordingly.

## Place the Documents for Ingestion

Add your source documents to the designated ingestion directory path -> `/var/lib/ai-services/<app-name>/docs/`. These files will be processed and indexed for retrieval by the RAG pipeline.

## Start Document Ingestion

Trigger the ingestion process to parse and upload the documents into DB. Once complete, the documents become searchable and ready for retrieval during chat interactions.

```bash
ai-services application start <app-name> --pod=<app-name>--ingest-docs
```

## Access the Chatbot

The chatbot URL is rendered on the terminal once the application is created and can also be viewed by using the `ai-service application info <app-name>` command.

```bash
$ ai-services application info rag-test
Application Name: rag-test
Application Template: RAG
Version: 0.0.1
Info:
-------
Day N:
1. Chatbot is available to use at http://10.20.177.252:3000
2. If you want to serve any more new documents via this RAG application, add them inside "/var/lib/ai-services/rag-test/docs" directory
3. If you want to do the ingestion again, execute below command and wait for the ingestion to be completed before accessing the chatbot to query the new data.
`ai-services application start rag-test --pod=rag-test--ingest-docs`
4. In case if you want to clean the documents added to the db, execute below command
`ai-services application start rag-test --pod=rag-test--clean-docs`
```
