Day N:

{{- if ne .API_URL "" }}
{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} API is available to use at {{ .API_URL }}. Use this endpoint for time-series forecasting via programmatic access. Send `Authorization: Bearer <token>` with every request. Get a token with `ai-services catalog service-token --service forecast`.
{{- else }}

- {{ .SERVICE_NAME }} API is unavailable to use. Please make sure 'forecast-api' pod is running.
{{- end }}
{{- end }}
