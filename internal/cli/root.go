package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "mnemo",
	Short: "Local-first agent memory and retrieval MCP server",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		level := viper.GetString("log_level")
		var logLevel slog.Level
		switch strings.ToLower(level) {
		case "debug":
			logLevel = slog.LevelDebug
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		default:
			logLevel = slog.LevelInfo
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
		return nil
	},
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().String("db-path", "", "path to SQLite database")
	rootCmd.PersistentFlags().String("ollama-host", "", "Ollama API base URL")
	rootCmd.PersistentFlags().String("embed-model", "", "embedding model name")
	rootCmd.PersistentFlags().Int("embed-dim", 0, "embedding dimension")
	rootCmd.PersistentFlags().String("log-level", "", "log level (debug, info, warn, error)")

	viper.BindPFlag("db_path", rootCmd.PersistentFlags().Lookup("db-path"))
	viper.BindPFlag("ollama_host", rootCmd.PersistentFlags().Lookup("ollama-host"))
	viper.BindPFlag("embed_model", rootCmd.PersistentFlags().Lookup("embed-model"))
	viper.BindPFlag("embed_dim", rootCmd.PersistentFlags().Lookup("embed-dim"))
	viper.BindPFlag("log_level", rootCmd.PersistentFlags().Lookup("log-level"))
}

func initConfig() {
	viper.SetEnvPrefix("MNEMO")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	viper.SetDefault("db_path", "./mnemo.db")
	viper.SetDefault("ollama_host", "https://ollama.cluster.collins.is")
	viper.SetDefault("embed_model", "nomic-embed-text")
	viper.SetDefault("embed_dim", 768)
	viper.SetDefault("ollama_timeout", "30s")
	viper.SetDefault("chunk_target_tokens", 400)
	viper.SetDefault("chunk_overlap", 40)
	viper.SetDefault("log_level", "info")
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}
