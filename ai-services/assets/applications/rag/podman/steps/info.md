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

{{- if ne .DIGITIZE_UI_PORT "" }}
{{- if eq .DIGITIZE_UI_STATUS "running" }}

- Digitize UI is available to use at http://{{ .HOST_IP }}:{{ .DIGITIZE_UI_PORT }}. Use this web interface to upload and manage documents for the RAG application.
{{- else }}

- Digitize UI is unavailable to use. Please make sure '{{ .AppName }}--digitize-api' pod is running.
{{- end }}
{{- end }}

{{- if ne .DIGITIZE_API_PORT "" }}
{{- if eq .DIGITIZE_API_STATUS "running" }}

- Digitize API is available to use at http://{{ .HOST_IP }}:{{ .DIGITIZE_API_PORT }}. Use this endpoint for programmatic access and direct API integration.
{{- else }}

- Digitize API is unavailable to use. Please make sure '{{ .AppName }}--digitize-api' pod is running.
{{- end }}
{{- end }}

- If you want to serve any more new documents via this RAG application using CLI, add them inside "/var/lib/ai-services/applications/{{ .AppName }}/docs" directory. If you want to do the ingestion again, execute below command and wait for the ingestion to be completed before accessing the chatbot to query the new data.
`ai-services application start {{ .AppName }} --pod={{ .AppName }}--ingest-docs`

- In case if you want to clean the documents added to the db, execute below command
`ai-services application start {{ .AppName }} --pod={{ .AppName }}--clean-docs`

{{- if eq .SUMMARIZE_API_STATUS "running" }}

- Summarize API is available to use at http://{{ .HOST_IP }}:{{ .SUMMARIZE_API_PORT }}. Use this endpoint for document summarization via programmatic access.
{{- else }}

- Summarize API is unavailable to use. Please make sure 'summarize-api' pod is running.
{{- end }}
