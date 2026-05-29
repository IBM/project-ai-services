Day N:

{{- if ne .SIMILARITY_API_PORT "" }}
{{- if eq .SIMILARITY_API_STATUS "running" }}

- Similarity Search API is available to use at http://{{ .HOST_IP }}:{{ .SIMILARITY_API_PORT }}.
{{- else }}

- Similarity Search API is unavailable to use. Please make sure '{{ .AppName }}--similarity-api' pod is running.
{{- end }}
{{- end }}
