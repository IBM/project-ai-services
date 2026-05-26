- Access the Catalog UI at https://{{ .CATALOG_UI_DOMAIN }}{{ if ne .HTTPS_PORT "443" }}:{{ .HTTPS_PORT }}{{ end }}

- Access the Catalog Backend at https://{{ .CATALOG_API_DOMAIN }}{{ if ne .HTTPS_PORT "443" }}:{{ .HTTPS_PORT }}{{ end }}
