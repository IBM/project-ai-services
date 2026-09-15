Day N:

{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} Embedding API is available to use at https://{{ .API_ROUTE }}
{{- else }}

- {{ .SERVICE_NAME }} Embedding API is unavailable to use. Please make sure the 'embedding-api' deployment is running.
{{- end }}
