package config

import (
	"fmt"
	"strconv"
)

type LookupEnvFunc func(string) (string, bool)

type Config struct {
	Server ServerConfig
	LLM    LLMConfig
	Agent  AgentConfig
	RAG    RAGConfig
	Milvus MilvusConfig
	Redis  RedisConfig
}

type ServerConfig struct {
	Port      int
	AuthToken string
}

type LLMConfig struct {
	Provider       string
	APIKey         string
	Model          string
	BaseURL        string
	MaxTokens      int
	TimeoutSeconds int
	MaxRetries     int
	RetryBackoffMS int
}

type AgentConfig struct {
	MaxSteps    int
	Temperature float64
}

type RAGConfig struct {
	EmbeddingAPIKey  string
	EmbeddingBaseURL string
	EmbeddingModel   string
	EmbeddingDim     int
	RerankAPIKey     string
	RerankBaseURL    string
	RerankModel      string
	TopK             int
	TopN             int
}

type MilvusConfig struct {
	Address    string
	Collection string
}

type RedisConfig struct {
	Address string
	DB      int
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			Port:      8080,
			AuthToken: "",
		},
		LLM: LLMConfig{
			Provider:       "openai",
			APIKey:         "",
			Model:          "gpt-4o-mini",
			BaseURL:        "",
			MaxTokens:      4096,
			TimeoutSeconds: 30,
			MaxRetries:     2,
			RetryBackoffMS: 200,
		},
		Agent: AgentConfig{
			MaxSteps:    10,
			Temperature: 0.1,
		},
		RAG: RAGConfig{
			EmbeddingAPIKey:  "",
			EmbeddingBaseURL: "",
			EmbeddingModel:   "bge-large-zh-v1.5",
			EmbeddingDim:     1024,
			RerankAPIKey:     "",
			RerankBaseURL:    "",
			RerankModel:      "qwen3-vl-rerank",
			TopK:             20,
			TopN:             5,
		},
		Milvus: MilvusConfig{
			Address:    "localhost:19530",
			Collection: "api_documents",
		},
		Redis: RedisConfig{
			Address: "localhost:6379",
			DB:      0,
		},
	}
}

func (c *Config) ApplyEnv(lookup LookupEnvFunc) error {
	if v, ok := lookup("PORT"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid PORT: %w", err)
		}
		c.Server.Port = n
	}
	if v, ok := lookup("AUTH_TOKEN"); ok {
		c.Server.AuthToken = v
	}
	if v, ok := lookup("LLM_API_KEY"); ok {
		c.LLM.APIKey = v
	}
	if v, ok := lookup("LLM_PROVIDER"); ok && v != "" {
		c.LLM.Provider = v
	}
	if v, ok := lookup("LLM_MODEL"); ok && v != "" {
		c.LLM.Model = v
	}
	if v, ok := lookup("LLM_BASE_URL"); ok && v != "" {
		c.LLM.BaseURL = v
	}
	if v, ok := lookup("LLM_MAX_TOKENS"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid LLM_MAX_TOKENS: %w", err)
		}
		c.LLM.MaxTokens = n
	}
	if v, ok := lookup("LLM_TIMEOUT_SECONDS"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid LLM_TIMEOUT_SECONDS: %w", err)
		}
		c.LLM.TimeoutSeconds = n
	}
	if v, ok := lookup("LLM_MAX_RETRIES"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid LLM_MAX_RETRIES: %w", err)
		}
		c.LLM.MaxRetries = n
	}
	if v, ok := lookup("LLM_RETRY_BACKOFF_MS"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid LLM_RETRY_BACKOFF_MS: %w", err)
		}
		c.LLM.RetryBackoffMS = n
	}
	if v, ok := lookup("MILVUS_ADDRESS"); ok && v != "" {
		c.Milvus.Address = v
	}
	if v, ok := lookup("REDIS_ADDRESS"); ok && v != "" {
		c.Redis.Address = v
	}
	if v, ok := lookup("EMBEDDING_API_KEY"); ok && v != "" {
		c.RAG.EmbeddingAPIKey = v
	}
	if v, ok := lookup("EMBEDDING_BASE_URL"); ok && v != "" {
		c.RAG.EmbeddingBaseURL = v
	}
	if v, ok := lookup("EMBEDDING_MODEL"); ok && v != "" {
		c.RAG.EmbeddingModel = v
	}
	if v, ok := lookup("EMBEDDING_DIM"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid EMBEDDING_DIM: %w", err)
		}
		c.RAG.EmbeddingDim = n
	}
	if v, ok := lookup("RERANK_API_KEY"); ok && v != "" {
		c.RAG.RerankAPIKey = v
	}
	if v, ok := lookup("RERANK_BASE_URL"); ok && v != "" {
		c.RAG.RerankBaseURL = v
	}
	if v, ok := lookup("RERANK_MODEL"); ok && v != "" {
		c.RAG.RerankModel = v
	}
	return nil
}
