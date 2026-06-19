package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/clcollins/mnemo/internal/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type recallInput struct {
	Query     string   `json:"query" jsonschema:"The search query to find relevant memories or documents"`
	Spaces    []string `json:"spaces,omitempty" jsonschema:"Memory spaces to search (default: all). Use [\"product\"] for corpus knowledge."`
	Limit     int      `json:"limit,omitempty" jsonschema:"Maximum number of results to return (default: 10)"`
	MaxTokens int      `json:"max_tokens,omitempty" jsonschema:"Token budget for result content (default: 2000)"`
}

type rememberInput struct {
	Text    string `json:"text" jsonschema:"The information to remember as a concise standalone statement"`
	Space   string `json:"space,omitempty" jsonschema:"Memory space (default: default). Use 'product' for corpus docs."`
	Source  string `json:"source,omitempty" jsonschema:"Where this information came from"`
	Speaker string `json:"speaker,omitempty" jsonschema:"Who provided this (human or assistant)"`
}

type forgetInput struct {
	ChunkID string `json:"chunk_id" jsonschema:"The ID of the memory chunk to forget"`
}

func NewServer(svc *memory.Service) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "mnemo",
		Version: "0.1.0",
	}, nil)

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "recall",
		Description: "Search memories and product knowledge. " +
			"Call with spaces:[\"product\"] to query the product corpus. " +
			"Call without spaces to search all memories. " +
			"Returns ranked chunks with content and source metadata.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, args recallInput) (*sdkmcp.CallToolResult, any, error) {
		limit := args.Limit
		if limit <= 0 {
			limit = 10
		}
		maxTokens := args.MaxTokens
		if maxTokens <= 0 {
			maxTokens = 2000
		}

		results, err := svc.Recall(ctx, args.Query, args.Spaces, limit, maxTokens)
		if err != nil {
			return errorResult(err), nil, nil
		}

		type resultItem struct {
			ID          string  `json:"id"`
			Content     string  `json:"content"`
			Source      string  `json:"source,omitempty"`
			HeadingPath string  `json:"heading_path,omitempty"`
			Category    string  `json:"category,omitempty"`
			Score       float64 `json:"score"`
		}

		items := make([]resultItem, len(results))
		for i, r := range results {
			items[i] = resultItem{
				ID:          r.Chunk.ID,
				Content:     r.Chunk.Content,
				Source:      r.Chunk.Source,
				HeadingPath: r.Chunk.HeadingPath,
				Category:    r.Chunk.Category,
				Score:       r.Score,
			}
		}

		return jsonResult(items)
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "remember",
		Description: "Store a memory for later recall. " +
			"Use for decisions, lessons learned, preferences, or important facts. " +
			"Each memory should be a concise, standalone statement.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, args rememberInput) (*sdkmcp.CallToolResult, any, error) {
		chunk, err := svc.Remember(ctx, args.Text, args.Space, args.Source, args.Speaker)
		if err != nil {
			return errorResult(err), nil, nil
		}

		resp := map[string]any{
			"id":       chunk.ID,
			"category": chunk.Category,
			"space":    args.Space,
		}
		return jsonResult(resp)
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "forget",
		Description: "Soft-delete a memory by its chunk ID. " +
			"Use when information is explicitly superseded or incorrect.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, args forgetInput) (*sdkmcp.CallToolResult, any, error) {
		if err := svc.Forget(ctx, args.ChunkID); err != nil {
			return errorResult(err), nil, nil
		}
		return jsonResult(map[string]string{"status": "forgotten", "chunk_id": args.ChunkID})
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory_status",
		Description: "Get memory system health and statistics, including per-space chunk counts.",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, args struct{}) (*sdkmcp.CallToolResult, any, error) {
		status, err := svc.Status(ctx)
		if err != nil {
			return errorResult(err), nil, nil
		}
		resp := map[string]any{
			"total_chunks": status.TotalChunks,
			"spaces":       status.SpaceCounts,
		}
		return jsonResult(resp)
	})

	return server
}

func jsonResult(v any) (*sdkmcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(err), nil, nil
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: string(data)},
		},
	}, nil, nil
}

func errorResult(err error) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: fmt.Sprintf("error: %v", err)},
		},
		IsError: true,
	}
}
