Day N:

{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} API is available to use at https://{{ .API_ROUTE }}. Use this endpoint for document summarization via programmatic access.
{{- else }}

- {{ .SERVICE_NAME }} API is unavailable to use. Please make sure the 'summarize-api' deployment is running.
{{- end }}
