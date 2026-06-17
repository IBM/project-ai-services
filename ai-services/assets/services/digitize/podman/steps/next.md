{{- if ne .UI_URL "" }}
- Add documents to your RAG application using the web interface at {{ .UI_URL }}.
{{- end }}
{{- if ne .API_URL "" }}
OR
- Use the {{ .SERVICE_NAME }} API for programmatic document upload at {{ .API_URL }}.
{{- end }}
