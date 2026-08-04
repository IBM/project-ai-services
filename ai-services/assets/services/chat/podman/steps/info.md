Day N:

{{- if ne .UI_URL "" }}
{{- if eq .UI_STATUS "running" }}

- {{ .SERVICE_NAME }} is available to use at {{ .UI_URL }}.
{{- else }}

- {{ .SERVICE_NAME }} is unavailable to use. Please make sure 'chat-bot' pod is running.
{{- end }}
{{- end }}

{{- if ne .API_URL "" }}
{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} API is available to use at {{ .API_URL }}.
{{- else }}

- {{ .SERVICE_NAME }} API is unavailable to use. Please make sure 'chat-bot' pod is running.
{{- end }}
{{- end }}
