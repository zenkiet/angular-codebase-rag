package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Project ProjectConfig `mapstructure:"project"`

	Embedding struct {
		Ollama    OllamaConfig  `mapstructure:"ollama"`
		BatchSize int           `mapstructure:"batch_size"`
		Timeout   time.Duration `mapstructure:"timeout"`
	}

	VectorStore struct {
		Qdrant QdrantConfig `mapstructure:"qdrant"`
	}

	Parser struct {
		WorkerCount int `mapstructure:"worker_count"`
		ChannelBuf  int `mapstructure:"channel_buffer"`
	}

	Chunking struct {
		MaxChars     int `mapstructure:"max_chars"`
		OverlapLines int `mapstructure:"overlap_lines"`
	}
}

type ProjectConfig struct {
	Name       string   `mapstructure:"project_name"`
	Root       string   `mapstructure:"repo_root"`
	Collection string   `mapstructure:"collection"`
	Exclude    []string `mapstructure:"exclude"`
}

type MCPConfig struct {
	DefaultLimit    int `mapstructure:"default_limit"`
	MaxLimit        int `mapstructure:"max_limit"`
	MaxContextLines int `mapstructure:"max_context_lines"`
}

type OllamaConfig struct {
	BaseURL string `mapstructure:"base_url"`
	Model   string `mapstructure:"model"`
}

type QdrantConfig struct {
	BaseURL    string `mapstructure:"base_url"`
	Collection string `mapstructure:"collection"`
	APIKey     string `mapstructure:"api_key"`
}

func Load(configPath string) (*Config, error) {
	v := viper.New()
	v.SetEnvPrefix("RAG")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	applyDefaults(v)

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.AddConfigPath(".")
		v.SetConfigName("config")
		v.SetConfigType("yaml")
	}
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && configPath != "" {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config unmarshal: %w", err)
	}

	if err := normalize(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(v *viper.Viper) {
	v.SetDefault("project.project_name", "angular-app")
	v.SetDefault("project.repo_root", ".")
	v.SetDefault("project.exclude", []string{})
	v.SetDefault("embedding.batch_size", 8)
	v.SetDefault("embedding.timeout", 30*time.Second)
	v.SetDefault("embedding.ollama.base_url", "http://localhost:11434")
	v.SetDefault("embedding.ollama.model", "nomic-embed-text")
	v.SetDefault("vectorstore.qdrant.base_url", "http://localhost:6333")
	v.SetDefault("vectorstore.qdrant.api_key", "")
	v.SetDefault("vectorstore.qdrant.collection", "angular_codebase")
	v.SetDefault("parser.worker_count", 5)
	v.SetDefault("parser.channel_buffer", 100)
	v.SetDefault("chunking.max_chars", 1500)
	v.SetDefault("chunking.overlap_lines", 20)
	v.SetDefault("mcp.default_limit", 8)
	v.SetDefault("mcp.max_limit", 20)
	v.SetDefault("mcp.max_context_lines", 80)
}

func normalize(cfg *Config) error {
	cfg.Project.Name = strings.TrimSpace(cfg.Project.Name)
	if cfg.Project.Name == "" {
		return fmt.Errorf("project.project_name is required")
	}
	if strings.TrimSpace(cfg.Project.Root) == "" {
		cfg.Project.Root = "."
	}
	absRoot, err := filepath.Abs(cfg.Project.Root)
	if err != nil {
		return fmt.Errorf("resolve project.repo_root: %w", err)
	}
	cfg.Project.Root = absRoot
	if cfg.Project.Collection != "" {
		cfg.Project.Collection = strings.TrimSpace(cfg.Project.Collection)
	}
	if cfg.VectorStore.Qdrant.Collection == "" {
		if cfg.Project.Collection != "" {
			cfg.VectorStore.Qdrant.Collection = cfg.Project.Collection
		} else {
			cfg.VectorStore.Qdrant.Collection = cfg.Project.Name
		}
	}
	if cfg.Embedding.BatchSize <= 0 {
		cfg.Embedding.BatchSize = 8
	}
	if cfg.Parser.WorkerCount <= 0 {
		cfg.Parser.WorkerCount = 5
	}
	if cfg.Parser.ChannelBuf <= 0 {
		cfg.Parser.ChannelBuf = 100
	}
	if cfg.Embedding.Timeout == 0 {
		cfg.Embedding.Timeout = 30 * time.Second
	}
	if cfg.Chunking.MaxChars <= 0 {
		cfg.Chunking.MaxChars = 1500
	}
	if cfg.Chunking.OverlapLines < 0 {
		cfg.Chunking.OverlapLines = 0
	}
	if cfg.MCP.DefaultLimit <= 0 {
		cfg.MCP.DefaultLimit = 8
	}
	if cfg.MCP.MaxLimit <= 0 {
		cfg.MCP.MaxLimit = 20
	}
	if cfg.MCP.DefaultLimit > cfg.MCP.MaxLimit {
		cfg.MCP.DefaultLimit = cfg.MCP.MaxLimit
	}
	if cfg.MCP.MaxContextLines <= 0 {
		cfg.MCP.MaxContextLines = 80
	}
	return nil
}
