- Add documents to your RAG application using the web interface at http://{{ .HOST_IP }}:{{ .DIGITIZE_UI_PORT }}.

- These documents are consumed by Q&A service.

- Use the Similarity API at http://{{ .HOST_IP }}:{{ .SIMILARITY_API_PORT }} to perform vector similarity search via programmatic access.

- Run "ai-services application info {{ .AppName }} --runtime podman" to view service endpoints.
