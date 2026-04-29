Day N:

{{- if ne .DIGITIZE_UI_PORT "" }}
{{- if eq .DIGITIZE_UI_STATUS "running" }}

- Add documents to your RAG application using the Digitize Documents UI: http://{{ .HOST_IP }}:{{ .DIGITIZE_UI_PORT }}.
{{- else }}

- Digitize Documents UI is unavailable to use. Please make sure '{{ .AppName }}--digitize-ui' pod is running.
{{- end }}
{{- end }}

{{- if ne .DIGITIZE_API_PORT "" }}
{{- if eq .DIGITIZE_API_STATUS "running" }}

- Digitize Documents API is available to use at http://{{ .HOST_IP }}:{{ .DIGITIZE_API_PORT }}. Use this endpoint for programmatic access and direct API integration.
{{- else }}

- Digitize Documents API is unavailable to use. Please make sure '{{ .AppName }}--digitize-backend-server' pod is running.
{{- end }}
{{- end }}
