Day N:

{{- if ne .CATALOG_UI_ROUTE "" }}
{{- if eq .UI_STATUS "running" }}

- Catalog UI is available at {{ .CATALOG_UI_ROUTE }}
{{- else }}

- Catalog UI is unavailable. Please make sure '{{ .AppName }}--catalog-ui' container is running.
{{- end }}
{{- end }}

{{- if ne .CATALOG_API_ROUTE "" }}
{{- if eq .BACKEND_STATUS "running" }}

- Catalog Backend API is available at {{ .CATALOG_API_ROUTE }}
{{- else }}

- Catalog Backend API is unavailable. Please make sure '{{ .AppName }}--catalog-backend' container is running.
{{- end }}
{{- end }}
