1. Move the documents that you want to serve via this RAG application inside "/var/lib/ai-services/{{ .AppName }}/docs" directory

2. Start the ingestion with below command to feed the documents placed in previous step into the DB
`ai-services application start {{ .AppName }} --pod={{ .AppName }}--ingest-docs`

3. Chatbot is available to use at http://{{ .HOST_IP }}:{{ .UI_PORT }}
