- Move the documents that you want to serve via this RAG application inside "/var/lib/ai-services/applications/{{ .AppName }}/docs" directory

- Start the ingestion with below command to feed the documents placed in previous step into the DB
`ai-services application start {{ .AppName }} --pod={{ .AppName }}--ingest-docs`

{{- if ne .UI_PORT "" }}

- Chatbot UI is available to use at http://{{ .HOST_IP }}:{{ .UI_PORT }}.
{{- end }}

{{- if ne .BACKEND_PORT "" }}

- Chatbot Backend is available to use at http://{{ .HOST_IP }}:{{ .BACKEND_PORT }}.
{{- end }}
