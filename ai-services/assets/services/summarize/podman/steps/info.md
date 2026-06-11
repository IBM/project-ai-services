Day N:

{{- if eq .STATUS "running" }}

- Summarize API is available to use at {{ .API_URL }}. Use this endpoint for document summarization via programmatic access.
{{- else }}

- Summarize API is unavailable to use. Please make sure 'summarize-api' pod is running.
{{- end }}
