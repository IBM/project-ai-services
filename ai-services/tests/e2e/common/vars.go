package common

import "time"

var (
	ExpectedPodSuffixes = map[string][]string{
		// Catalog-path (podman): pod names match the catalog service/component IDs.
		//
		// Observed from `podman ps` after 'application create --runtime podman':
		//   opensearch-<slug>        ← vector store component
		//   llm-<slug>               ← LLM inference (vLLM) component
		//   embedding-<slug>         ← embedding model component
		//   reranker-<slug>          ← reranker model component
		//   similarity-api-<slug>    ← similarity service
		//   chat-bot-<slug>          ← chat service (UI + backend-server pods)
		//   digitize-<slug>          ← digitize service (UI + backend-server pods)
		"podman": {
			"opensearch",
			"llm",
			"embedding",
			"reranker",
			"similarity-api",
			"chat-bot",
			"digitize",
		},
		"openshift": {
			"backend",
			"digitize-api",
			"digitize-ui",
			"embedding-predictor",
			"instruct-predictor",
			"opensearch",
			"reranker-predictor",
			"ui",
		},
	}
	DeleteSleepInterval = 10 * time.Second
)
