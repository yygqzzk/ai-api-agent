package config

import (
	"github.com/caarlos0/env/v11"
)

type Config struct {
	Server ServerConfig
	LLM    LLMConfig
	Agent  AgentConfig
	RAG    RAGConfig
	Milvus MilvusConfig
	Redis  RedisConfig
}

type ServerConfig struct {
	Port      int    `env:"PORT"       envDefault:"8080"`
	AuthToken string `env:"AUTH_TOKEN"`
}

type LLMConfig struct {
	Provider       string `env:"LLM_PROVIDER"        envDefault:"openai"`
	APIKey         string `env:"LLM_API_KEY"`
	Model          string `env:"LLM_MODEL"            envDefault:"gpt-4o-mini"`
	BaseURL        string `env:"LLM_BASE_URL"`
	MaxTokens      int    `env:"LLM_MAX_TOKENS"       envDefault:"4096"`
	TimeoutSeconds int    `env:"LLM_TIMEOUT_SECONDS"  envDefault:"30"`
	MaxRetries     int    `env:"LLM_MAX_RETRIES"      envDefault:"2"`
	RetryBackoffMS int    `env:"LLM_RETRY_BACKOFF_MS" envDefault:"200"`
}

type AgentConfig struct {
	MaxSteps    int     `env:"AGENT_MAX_STEPS"    envDefault:"10"`
	Temperature float64 `env:"AGENT_TEMPERATURE"  envDefault:"0.1"`
}

type RAGConfig struct {
	EmbeddingAPIKey  string `env:"EMBEDDING_API_KEY"`
	EmbeddingBaseURL string `env:"EMBEDDING_BASE_URL"`
	EmbeddingModel   string `env:"EMBEDDING_MODEL"  envDefault:"bge-large-zh-v1.5"`
	EmbeddingDim     int    `env:"EMBEDDING_DIM"    envDefault:"1024"`
	RerankAPIKey     string `env:"RERANK_API_KEY"`
	RerankBaseURL    string `env:"RERANK_BASE_URL"`
	RerankModel      string `env:"RERANK_MODEL"     envDefault:"qwen3-vl-rerank"`
	TopK             int    `env:"RAG_TOP_K"        envDefault:"20"`
	TopN             int    `env:"RAG_TOP_N"        envDefault:"5"`
}

type MilvusConfig struct {
	Address    string `env:"MILVUS_ADDRESS"    envDefault:"localhost:19530"`
	Collection string `env:"MILVUS_COLLECTION" envDefault:"api_documents"`
}

type RedisConfig struct {
	Address  string `env:"REDIS_ADDRESS"  envDefault:"localhost:6379"`
	Password string `env:"REDIS_PASSWORD"`
	DB       int    `env:"REDIS_DB"       envDefault:"0"`
}

// LoadFromEnv 从环境变量加载配置（替代原 Default() + ApplyEnv()）
func LoadFromEnv() (Config, error) {
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
