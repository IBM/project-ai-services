Day N:

{{- if eq .UI_STATUS "running" }}

- Chatbot UI is available to use at http://{{ .UI_ROUTE }}.
{{- else }}

- Chatbot UI is unavailable to use. Please make sure 'ui' pod is running.
{{- end }}

{{- if eq .BACKEND_STATUS "running" }}

- Chatbot Backend is available to use at http://{{ .BACKEND_ROUTE }}.
{{- else }}

- Chatbot Backend is unavailable to use. Please make sure 'backend' pod is running.
{{- end }}
