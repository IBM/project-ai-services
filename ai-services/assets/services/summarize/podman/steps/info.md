Day N:

{{- if eq .SUMMARIZE_API_STATUS "running" }}

- Summarize API is available to use at http://{{ .HOST_IP }}:{{ .SUMMARIZE_API_PORT }}. Use this endpoint for document summarization via programmatic access.
{{- else }}

- Summarize API is unavailable to use. Please make sure '{{ .AppName }}--summarize-api' pod is running.
{{- end }}
