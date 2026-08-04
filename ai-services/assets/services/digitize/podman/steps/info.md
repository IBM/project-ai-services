Day N:

{{- if ne .UI_URL "" }}
{{- if eq .UI_STATUS "running" }}

- Add documents to your RAG application using the {{ .SERVICE_NAME }} Documents UI: {{ .UI_URL }}.
{{- else }}

- {{ .SERVICE_NAME }} Documents UI is unavailable to use. Please make sure 'digitize-ui' pod is running.
{{- end }}
{{- end }}

{{- if ne .API_URL "" }}
{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} Documents API is available to use at {{ .API_URL }}. Use this endpoint for programmatic access and direct API integration.
{{- else }}

- {{ .SERVICE_NAME }} Documents API is unavailable to use. Please make sure 'digitize-backend-server' pod is running.
{{- end }}
{{- end }}
