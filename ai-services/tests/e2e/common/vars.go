package common

var (
	ExpectedPodSuffixes = map[string][]string{
		"podman": {
			"opensearch",
			"summarize-api",
			"digitize",
			"vllm-server",
			"clean-docs",
			"ingest-docs",
			"chat-bot",
		},
		"openshift": {
			"backend",
			"digitize-api",
			"digitize-ui",
			"embedding-predictor",
			"instruct-predictor",
			"opensearch",
			"reranker-predictor",
			"summarize-api",
			"ui",
		},
	}
)
