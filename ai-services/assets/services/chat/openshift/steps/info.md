Day N:

{{- if eq .UI_STATUS "running" }}

- {{ .SERVICE_NAME }} is available to use at https://{{ .UI_ROUTE }}.
{{- else }}

- {{ .SERVICE_NAME }} is unavailable to use. Please make sure the 'ui' container in the 'chat-bot' deployment is running.
{{- end }}

{{- if eq .BACKEND_STATUS "running" }}

- {{ .SERVICE_NAME }} API is available to use at https://{{ .BACKEND_ROUTE }}.
{{- else }}

- {{ .SERVICE_NAME }} API is unavailable to use. Please make sure the 'backend-server' container in the 'chat-bot' deployment is running.
{{- end }}
