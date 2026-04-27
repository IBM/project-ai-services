Day N:

{{- if ne .UI_PORT "" }}
{{- if eq .UI_STATUS "running" }}

- Q&A Chatbot (Dev) is available to use at http://{{ .HOST_IP }}:{{ .UI_PORT }}.
{{- else }}

- Q&A Chatbot (Dev) is unavailable to use. Please make sure '{{ .AppName }}--chat-bot-dev' pod is running.
{{- end }}
{{- end }}

{{- if ne .BACKEND_PORT "" }}
{{- if eq .BACKEND_STATUS "running" }}

- Q&A API (Dev) is available to use at http://{{ .HOST_IP }}:{{ .BACKEND_PORT }}.
{{- else }}

- Q&A API (Dev) is unavailable to use. Please make sure '{{ .AppName }}--chat-bot-dev' pod is running.
{{- end }}
{{- end }}
