Day N:

{{- if ne .UI_PORT "" }}
{{- if eq .UI_STATUS "running" }}

- Chatbot UI is available to use at http://{{ .HOST_IP }}:{{ .UI_PORT }}.
{{- else }}

- Chatbot UI is unavailable to use. Please make sure '{{ .AppName }}--chat-bot' pod is running.
{{- end }}
{{- end }}

{{- if ne .BACKEND_PORT "" }}
{{- if eq .BACKEND_STATUS "running" }}

- Chatbot Backend is available to use at http://{{ .HOST_IP }}:{{ .BACKEND_PORT }}.
{{- else }}

- Chatbot Backend is unavailable to use. Please make sure '{{ .AppName }}--chat-bot' pod is running.
{{- end }}
{{- end }}

- If you want to serve any more new documents via this RAG application, add them inside "/var/lib/ai-services/applications/{{ .AppName }}/docs" directory

- If you want to do the ingestion again, execute below command and wait for the ingestion to be completed before accessing the chatbot to query the new data.
`ai-services application start {{ .AppName }} --pod={{ .AppName }}--ingest-docs`

- In case if you want to clean the documents added to the db, execute below command
`ai-services application start {{ .AppName }} --pod={{ .AppName }}--clean-docs`
