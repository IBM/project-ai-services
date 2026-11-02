Day N:

{{- if ne .UI_URL "" }}
{{- if eq .UI_STATUS "running" }}

- Add documents to your RAG application using the {{ .SERVICE_NAME }} Documents UI: {{ .UI_URL }}.
{{- else }}

- {{ .SERVICE_NAME }} Documents UI is unavailable to use. Please make sure the 'digitize-ui' pod is running.
{{- end }}
{{- end }}

{{- if ne .API_URL "" }}
{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} Documents API is available to use at {{ .API_URL }}. Use this endpoint for programmatic access and direct API integration.
{{- else }}

- {{ .SERVICE_NAME }} Documents API is unavailable to use. Please make sure the 'digitize-api' pod is running.
{{- end }}
{{- end }}

{{- if .MCP_URL }}
{{- if eq .MCP_STATUS "running" }}

- {{ .SERVICE_NAME }} MCP server is available to use at {{ .MCP_URL }}/mcp. Point an MCP client at this endpoint to call the API as tools.
{{- else }}

- {{ .SERVICE_NAME }} MCP server is unavailable to use. Please make sure the 'mcp-sidecar' container in the 'digitize-api' pod is running.
{{- end }}
{{- end }}
