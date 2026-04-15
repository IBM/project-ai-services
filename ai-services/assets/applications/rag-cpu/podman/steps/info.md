Day N:

{{- if ne .UI_PORT "" }}
{{- if eq .UI_STATUS "running" }}

- Q&A Chatbot is available to use at http://{{ .HOST_IP }}:{{ .UI_PORT }}.
{{- else }}

- Q&A Chatbot is unavailable to use. Please make sure '{{ .AppName }}--chat-bot' pod is running.
{{- end }}
{{- end }}

{{- if ne .BACKEND_PORT "" }}
{{- if eq .BACKEND_STATUS "running" }}

- Q&A API is available to use at http://{{ .HOST_IP }}:{{ .BACKEND_PORT }}.
{{- else }}

- Q&A API is unavailable to use. Please make sure '{{ .AppName }}--chat-bot' pod is running.
{{- end }}
{{- end }}

{{- if ne .DIGITIZE_UI_PORT "" }}
{{- if eq .DIGITIZE_UI_STATUS "running" }}

- Add documents to your RAG application using the Digitize Documents UI: http://{{ .HOST_IP }}:{{ .DIGITIZE_UI_PORT }}.
{{- else }}

- Digitize Documents UI is unavailable to use. Please make sure '{{ .AppName }}--digitize-api' pod is running.
{{- end }}
{{- end }}

{{- if ne .DIGITIZE_API_PORT "" }}
{{- if eq .DIGITIZE_API_STATUS "running" }}

- Digitize Documents API is available to use at http://{{ .HOST_IP }}:{{ .DIGITIZE_API_PORT }}. Use this endpoint for programmatic access and direct API integration.
{{- else }}

- Digitize Documents API is unavailable to use. Please make sure '{{ .AppName }}--digitize-api' pod is running.
{{- end }}
{{- end }}

{{- if eq .SUMMARIZE_API_STATUS "running" }}

- Summarize API is available to use at http://{{ .HOST_IP }}:{{ .SUMMARIZE_API_PORT }}. Use this endpoint for document summarization via programmatic access.
{{- else }}

- Summarize API is unavailable to use. Please make sure 'summarize-api' pod is running.
{{- end }}
