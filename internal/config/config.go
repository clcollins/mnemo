package config

import "time"

type Config struct {
	DBPath            string        `mapstructure:"db_path"`
	OllamaHost        string        `mapstructure:"ollama_host"`
	EmbedModel        string        `mapstructure:"embed_model"`
	EmbedDim          int           `mapstructure:"embed_dim"`
	OllamaTimeout     time.Duration `mapstructure:"ollama_timeout"`
	ChunkTargetTokens int           `mapstructure:"chunk_target_tokens"`
	ChunkOverlap      int           `mapstructure:"chunk_overlap"`
	LogLevel          string        `mapstructure:"log_level"`
}

func Defaults() Config {
	return Config{
		DBPath:            "./mnemo.db",
		OllamaHost:        "https://ollama.cluster.collins.is",
		EmbedModel:        "nomic-embed-text",
		EmbedDim:          768,
		OllamaTimeout:     30 * time.Second,
		ChunkTargetTokens: 400,
		ChunkOverlap:      40,
		LogLevel:          "info",
	}
}
