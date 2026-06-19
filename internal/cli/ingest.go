package cli

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/clcollins/mnemo/internal/embed"
	"github.com/clcollins/mnemo/internal/ingest"
	"github.com/clcollins/mnemo/internal/store/sqlite"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var ingestCmd = &cobra.Command{
	Use:   "ingest <path> [path...]",
	Short: "Ingest documents into the product corpus",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := viper.GetString("db_path")
		ollamaHost := viper.GetString("ollama_host")
		embedModel := viper.GetString("embed_model")
		embedDim := viper.GetInt("embed_dim")
		timeout := viper.GetDuration("ollama_timeout")
		targetTokens := viper.GetInt("chunk_target_tokens")
		overlap := viper.GetInt("chunk_overlap")

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

		ctx := context.Background()
		var totalFiles, totalAdded, totalSkipped int

		for _, path := range args {
			slog.Info("ingesting", "path", path)
			result, err := ingest.Ingest(ctx, path, embedder, store, targetTokens, overlap)
			if err != nil {
				return fmt.Errorf("ingest %s: %w", path, err)
			}
			totalFiles += result.FilesProcessed
			totalAdded += result.ChunksAdded
			totalSkipped += result.ChunksSkipped
		}

		fmt.Printf("Ingestion complete: %d files, %d chunks added, %d chunks skipped\n",
			totalFiles, totalAdded, totalSkipped)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(ingestCmd)
}
