Day N:

{{- if ne .API_URL "" }}
{{- if eq .STATUS "running" }}

- Similarity Search API is available to use at {{ .API_URL }}
{{- else }}

- Similarity Search API is unavailable to use. Please make sure 'similarity-api' pod is running.
{{- end }}
{{- end }}
