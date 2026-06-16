{{- if ne .UI_URL "" }}
- Add documents to your RAG application using the web interface at {{ .UI_URL }}.
{{- end }}
{{- if ne .API_URL "" }}

- Use the Digitize API for programmatic document upload at {{ .API_URL }}.
{{- end }}

- These documents are consumed by Q&A service.
