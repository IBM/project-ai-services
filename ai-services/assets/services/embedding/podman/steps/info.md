Day N:

{{- if ne .API_URL "" }}
{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} Embedding API is available to use at {{ .API_URL }}
{{- else }}

- {{ .SERVICE_NAME }} Embedding API is unavailable to use. Please make sure 'embedding-api' pod is running.
{{- end }}
{{- end }}
