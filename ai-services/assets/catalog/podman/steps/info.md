Day N:

{{- if ne .CATALOG_UI_DOMAIN "" }}
{{- if eq .UI_STATUS "running" }}

- Catalog UI is available at https://{{ .CATALOG_UI_DOMAIN }}{{ if ne .HTTPS_PORT "443" }}:{{ .HTTPS_PORT }}{{ end }}
{{- else }}

- Catalog UI is unavailable. Please make sure '{{ .AppName }}--catalog-ui' container is running.
{{- end }}
{{- end }}

{{- if ne .CATALOG_API_DOMAIN "" }}
{{- if eq .BACKEND_STATUS "running" }}

- Catalog Backend API is available at https://{{ .CATALOG_API_DOMAIN }}{{ if ne .HTTPS_PORT "443" }}:{{ .HTTPS_PORT }}{{ end }}
{{- else }}

- Catalog Backend API is unavailable. Please make sure '{{ .AppName }}--catalog-backend' container is running.
{{- end }}
{{- end }}