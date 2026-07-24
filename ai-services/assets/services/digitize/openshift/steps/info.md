Day N:

{{- if eq .DIGITIZE_UI_STATUS "running" }}

- Add documents to your RAG application using the {{ .SERVICE_NAME }} Documents UI: https://{{ .DIGITIZE_UI_ROUTE }}.
{{- else }}

- {{ .SERVICE_NAME }} Documents UI is unavailable to use. Please make sure the 'digitize-ui' deployment is running.
{{- end }}

{{- if eq .DIGITIZE_API_STATUS "running" }}

- {{ .SERVICE_NAME }} Documents API is available to use at https://{{ .DIGITIZE_API_ROUTE }}. Use this endpoint for programmatic access and direct API integration.
{{- else }}

- {{ .SERVICE_NAME }} Documents API is unavailable to use. Please make sure the 'digitize-api' deployment is running.
{{- end }}
