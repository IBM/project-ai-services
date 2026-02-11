package common

var (
	ExpectedPodSuffixes = []string{
		"vllm-server",
		// "milvus", --commented as currently switch to opensearch is in-progress
		"clean-docs",
		"ingest-docs",
		"chat-bot",
	}
)
