Day N:

{{- if eq .UI_STATUS "running" }}

- Catalog UI is available at https://{{ .CATALOG_UI_ROUTE }}
{{- else }}

- Catalog UI is unavailable. Please make sure the 'ui' container in the 'catalog' pod is running.
{{- end }}

{{- if eq .BACKEND_STATUS "running" }}

- Catalog Backend API is available at https://{{ .CATALOG_API_ROUTE }}
{{- else }}

- Catalog Backend API is unavailable. Please make sure the 'backend' container in the 'catalog' pod is running.
{{- end }}
