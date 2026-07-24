Day N:

{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} Search API is available to use at https://{{ .API_ROUTE }}
{{- else }}

- {{ .SERVICE_NAME }} Search API is unavailable to use. Please make sure the 'similarity-api' deployment is running.
{{- end }}
