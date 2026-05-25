- Access the Catalog UI at https://{{ .CATALOG_UI_DOMAIN }}:{{ if eq .HTTPS_PORT "" }}443{{ else }}{{ .HTTPS_PORT }}{{ end }}

- Access the Catalog Backend at https://{{ .CATALOG_API_DOMAIN }}:{{ if eq .HTTPS_PORT "" }}443{{ else }}{{ .HTTPS_PORT }}{{ end }}
