Day N:

{{- if ne .UI_URL "" }}
{{- if eq .STATUS "running" }}

- Q&A Chatbot is available to use at {{ .UI_URL }}.
{{- else }}

- Q&A Chatbot is unavailable to use. Please make sure 'chat-bot' pod is running.
{{- end }}
{{- end }}

{{- if ne .API_URL "" }}
{{- if eq .STATUS "running" }}

- Q&A API is available to use at {{ .API_URL }}.
{{- else }}

- Q&A API is unavailable to use. Please make sure 'chat-bot' pod is running.
{{- end }}
{{- end }}
