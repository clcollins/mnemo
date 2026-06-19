package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/clcollins/mnemo/internal/embed"
	"github.com/clcollins/mnemo/internal/memory"
	"github.com/clcollins/mnemo/internal/store/sqlite"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTestSession(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := sqlite.New(dbPath, 4)
	if err != nil {
		t.Fatalf("New store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	svc := memory.NewService(embed.NewFake(4), s)
	server := NewServer(svc)

	ct, st := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()

	serverSession, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })

	return clientSession
}

func TestRecallTool(t *testing.T) {
	session := newTestSession(t)
	ctx := context.Background()

	_, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "remember",
		Arguments: map[string]any{
			"text":   "kubernetes operator migration steps",
			"source": "test",
		},
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "recall",
		Arguments: map[string]any{
			"query": "operator migration",
		},
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if result.IsError {
		t.Fatalf("recall returned error: %v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected recall content")
	}
}

func TestRememberTool(t *testing.T) {
	session := newTestSession(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "remember",
		Arguments: map[string]any{
			"text":    "We decided to use RRF fusion for search",
			"space":   "default",
			"source":  "test",
			"speaker": "human",
		},
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if result.IsError {
		t.Fatalf("remember returned error: %v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected remember response content")
	}
}

func TestForgetTool(t *testing.T) {
	session := newTestSession(t)
	ctx := context.Background()

	remResult, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "remember",
		Arguments: map[string]any{
			"text": "temporary note to forget",
		},
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	var remResp struct {
		ID string `json:"id"`
	}
	for _, c := range remResult.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			json.Unmarshal([]byte(tc.Text), &remResp)
			break
		}
	}
	if remResp.ID == "" {
		t.Fatal("could not extract chunk ID from remember response")
	}

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "forget",
		Arguments: map[string]any{
			"chunk_id": remResp.ID,
		},
	})
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if result.IsError {
		t.Fatalf("forget returned error: %v", result.Content)
	}
}

func TestMemoryStatusTool(t *testing.T) {
	session := newTestSession(t)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_status",
	})
	if err != nil {
		t.Fatalf("memory_status: %v", err)
	}
	if result.IsError {
		t.Fatalf("memory_status returned error: %v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected status content")
	}

	var statusResp map[string]any
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			if err := json.Unmarshal([]byte(tc.Text), &statusResp); err == nil {
				break
			}
		}
	}
	if statusResp == nil {
		t.Fatal("could not parse status response as JSON")
	}
}
