Day N:

{{- if ne .API_URL "" }}
{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} API is available to use at {{ .API_URL }}. Use this endpoint for document translation via programmatic access.
{{- else }}

- {{ .SERVICE_NAME }} API is unavailable to use. Please make sure 'translate-api' pod is running.
{{- end }}
{{- end }}
