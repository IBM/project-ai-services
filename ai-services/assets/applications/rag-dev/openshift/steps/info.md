Day N:

{{- if eq .UI_STATUS "running" }}

- Chatbot UI is available to use at https://{{ .UI_ROUTE }}.
{{- else }}

- Chatbot UI is unavailable to use. Please make sure 'ui' pod is running.
{{- end }}

{{- if eq .BACKEND_STATUS "running" }}

- Chatbot Backend is available to use at https://{{ .BACKEND_ROUTE }}.
{{- else }}

- Chatbot Backend is unavailable to use. Please make sure 'backend' pod is running.
{{- end }}

{{- if eq .SUMMARIZE_API_STATUS "running" }}

- Summarize API is available to use at https://{{ .SUMMARIZE_API_ROUTE }}. Use this endpoint for document summarization via programmatic access.
{{- else }}

- Summarize API is unavailable to use. Please make sure 'summarize-api' pod is running.
{{- end }}
