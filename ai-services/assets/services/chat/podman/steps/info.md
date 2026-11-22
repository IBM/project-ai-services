Day N:

{{- if ne .UI_URL "" }}
{{- if eq .UI_STATUS "running" }}

- {{ .SERVICE_NAME }} is available to use at {{ .UI_URL }}.
{{- else }}

- {{ .SERVICE_NAME }} is unavailable to use. Please make sure 'chat-bot' pod is running.
{{- end }}
{{- end }}

{{- if ne .API_URL "" }}
{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} API is available to use at {{ .API_URL }}. Send `Authorization: Bearer <token>` with every request. Get a token with `ai-services catalog service-token --service chat`.
{{- else }}

- {{ .SERVICE_NAME }} API is unavailable to use. Please make sure 'chat-bot' pod is running.
{{- end }}
{{- end }}

{{- if .MCP_URL }}
{{- if eq .MCP_STATUS "running" }}

- {{ .SERVICE_NAME }} MCP server is available to use at {{ .MCP_URL }}/mcp. Point an MCP client at this endpoint to call the API as tools. The MCP client must send the same kind of token in its `Authorization` header.
{{- else }}

- {{ .SERVICE_NAME }} MCP server is unavailable to use. Please make sure the 'mcp-sidecar' container in the 'chat-bot' pod is running.
{{- end }}
{{- end }}
