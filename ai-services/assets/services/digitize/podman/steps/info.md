Day N:

{{- if ne .UI_URL "" }}
{{- if eq .STATUS "running" }}

- Add documents to your RAG application using the Digitize Documents UI: {{ .UI_URL }}.
{{- else }}

- Digitize Documents UI is unavailable to use. Please make sure 'digitize-ui' pod is running.
{{- end }}
{{- end }}

{{- if ne .API_URL "" }}
{{- if eq .STATUS "running" }}

- Digitize Documents API is available to use at {{ .API_URL }}. Use this endpoint for programmatic access and direct API integration.
{{- else }}

- Digitize Documents API is unavailable to use. Please make sure 'digitize-backend-server' pod is running.
{{- end }}
{{- end }}
