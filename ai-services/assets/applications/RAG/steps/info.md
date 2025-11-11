Day N:

1. Chatbot is available to use at http://{{ .HOST_IP }}:{{ .UI_PORT }}

2. If you want to serve any more new documents via this RAG application, add them inside "/var/lib/ai-services/{{ .AppName }}/docs" directory

3. If you want to do the ingestion again, execute below command and wait for the ingestion to be completed before accessing the chatbot to query the new data.
`ai-services application start {{ .AppName }} --pod={{ .AppName }}--ingest-docs`

4. In case if you want to clean the documents added to the db, execute below command
`ai-services application start {{ .AppName }} --pod={{ .AppName }}--clean-docs`
