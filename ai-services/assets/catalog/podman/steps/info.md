Day N:

{{- if ne .UI_PORT "" }}
{{- if eq .UI_STATUS "running" }}

- Catalog UI is available at http://{{ .HOST_IP }}:{{ .UI_PORT }}
{{- else }}

- Catalog UI is unavailable. Please make sure '{{ .AppName }}--catalog' pod is running.
{{- end }}
{{- end }}

{{- if ne .BACKEND_PORT "" }}
{{- if eq .BACKEND_STATUS "running" }}

- Catalog Backend API is available at http://{{ .HOST_IP }}:{{ .BACKEND_PORT }}
{{- else }}

- Catalog Backend API is unavailable. Please make sure '{{ .AppName }}--catalog' pod is running.
{{- end }}
{{- end }}