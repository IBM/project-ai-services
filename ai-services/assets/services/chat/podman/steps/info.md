Day N:

{{- if ne .UI_PORT "" }}
{{- if eq .UI_STATUS "running" }}

- Q&A Chatbot is available to use at http://{{ .HOST_IP }}:{{ .UI_PORT }}.
{{- else }}

- Q&A Chatbot is unavailable to use. Please make sure '{{ .AppName }}--chat' pod is running.
{{- end }}
{{- end }}

{{- if ne .BACKEND_PORT "" }}
{{- if eq .BACKEND_STATUS "running" }}

- Q&A API is available to use at http://{{ .HOST_IP }}:{{ .BACKEND_PORT }}.
{{- else }}

- Q&A API is unavailable to use. Please make sure '{{ .AppName }}--chat' pod is running.
{{- end }}
{{- end }}