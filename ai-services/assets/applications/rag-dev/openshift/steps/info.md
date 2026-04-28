Day N:

{{- if eq .UI_STATUS "running" }}

- Q&A Chatbot is available to use at https://{{ .UI_ROUTE }}.
{{- else }}

- Q&A Chatbot is unavailable to use. Please make sure 'ui' pod is running.
{{- end }}

{{- if eq .BACKEND_STATUS "running" }}

- Q&A API is available to use at https://{{ .BACKEND_ROUTE }}.
{{- else }}

- Q&A API is unavailable to use. Please make sure 'backend' pod is running.
{{- end }}

{{- if .Values.instruct.apiKey }}

- vLLM exposed via Q&A is authenticated with user provided API Key: {{ .Values.instruct.apiKey }}
{{- end }}

{{- if eq .DIGITIZE_UI_STATUS "running" }}

- Add documents to your RAG application using the Digitize Documents UI: https://{{ .DIGITIZE_UI_ROUTE }}.
{{- else }}

- Digitize Documents UI is unavailable to use. Please make sure 'digitize-ui' pod is running.
{{- end }}

{{- if eq .DIGITIZE_API_STATUS "running" }}

- Digitize Documents API is available to use at https://{{ .DIGITIZE_API_ROUTE }}. Use this endpoint for programmatic access and direct API integration.
{{- else }}

- Digitize Documents API is unavailable to use. Please make sure 'digitize-api' pod is running.
{{- end }}

{{- if eq .SUMMARIZE_API_STATUS "running" }}

- Summarize API is available to use at https://{{ .SUMMARIZE_API_ROUTE }}. Use this endpoint for document summarization via programmatic access.
{{- else }}

- Summarize API is unavailable to use. Please make sure 'summarize-api' pod is running.
{{- end }}

{{- if eq .SIMILARITY_API_STATUS "running" }}

- Similarity API is available to use at https://{{ .SIMILARITY_API_ROUTE }}. Use this endpoint for vector similarity search via programmatic access.
{{- else }}

- Similarity API is unavailable to use. Please make sure 'similarity-api' pod is running.
{{- end }}
