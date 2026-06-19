package cli

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/clcollins/mnemo/internal/embed"
	mcpserver "github.com/clcollins/mnemo/internal/mcp"
	"github.com/clcollins/mnemo/internal/memory"
	"github.com/clcollins/mnemo/internal/store/sqlite"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server on stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := viper.GetString("db_path")
		ollamaHost := viper.GetString("ollama_host")
		embedModel := viper.GetString("embed_model")
		embedDim := viper.GetInt("embed_dim")
		timeout := viper.GetDuration("ollama_timeout")

		slog.Info("starting mnemo",
			"db", dbPath,
			"ollama", ollamaHost,
			"model", embedModel,
			"dim", embedDim,
		)

		store, err := sqlite.New(dbPath, embedDim)
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		defer store.Close()

		if err := store.Migrate(context.Background()); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}

		embedder, err := embed.NewOllama(ollamaHost, embedModel, embedDim, timeout)
		if err != nil {
			return fmt.Errorf("create embedder: %w", err)
		}

		svc := memory.NewService(embedder, store)
		server := mcpserver.NewServer(svc)

		slog.Info("mnemo MCP server running on stdio")
		if err := server.Run(context.Background(), &sdkmcp.StdioTransport{}); err != nil {
			return fmt.Errorf("server run: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	_ = time.Second // keep time import for duration parsing
}
