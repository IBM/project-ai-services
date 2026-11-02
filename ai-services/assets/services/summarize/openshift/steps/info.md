Day N:

{{- if ne .API_URL "" }}
{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} API is available to use at {{ .API_URL }}. Use this endpoint for document summarization via programmatic access.
{{- else }}

- {{ .SERVICE_NAME }} API is unavailable to use. Please make sure the 'summarize-api' pod is running.
{{- end }}
{{- end }}

{{- if .MCP_URL }}
{{- if eq .MCP_STATUS "running" }}

- {{ .SERVICE_NAME }} MCP server is available to use at {{ .MCP_URL }}/mcp. Point an MCP client at this endpoint to call the API as tools.
{{- else }}

- {{ .SERVICE_NAME }} MCP server is unavailable to use. Please make sure the 'mcp-sidecar' container in the 'summarize-api' pod is running.
{{- end }}
{{- end }}
