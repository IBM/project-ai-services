{{- if ne .UI_URL "" }}
- Access the Q&A Chatbot web interface at {{ .UI_URL }}.
{{- end }}
{{- if ne .API_URL "" }}

- Access the Q&A Chatbot API at {{ .API_URL }}.
{{- end }}
