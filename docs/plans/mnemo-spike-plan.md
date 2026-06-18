# Mnemo Spike Plan — Local-First RAG MCP Server

## Context

We have a large product corpus (~187 files, 3.2 MB) from `/product-map` output
and Wiki docs that agents need to query without dumping entire .md files into
context. Mnemo is a Go MCP server that pre-digests this corpus into chunks and
serves hybrid retrieval (vector + keyword + RRF fusion) over stdio.

This is a **2-day spike** to prove retrieval quality and validate the
architecture. Not a production system — a working POC on the laptop pointed at
the homelab Ollama endpoint.

**Dual purpose:** Team Placeholder Agentic SDLC investigation (work) and
personal GORT framework deployment (homelab). The interfaces are designed to
accommodate a Pi target later without implementing it now.

**What the original plan got right:** architecture, interfaces, data model, RRF
fusion constants. **What we're cutting:** Pi cross-compile, HTTPS CA/auth/proxy
complexity, query expansion, shared backend roadmap, eval harness, 10-milestone
structure. See `docs/plans/mnemo-implementation-plan.md` for the full original.

> **Supersedes:** nothing (first plan). The original implementation plan is
> preserved as a reference/aspirational document.

---

## Pre-requisites (before writing code)

1. Commit CONVENTIONS.md to main (already in worktree)
2. Create feature branch `spike/mnemo-poc`
3. Ensure `nomic-embed-text` model is pulled on the homelab Ollama at
   `https://ollama.cluster.collins.is` — either via `ollama pull` exec or
   the API. The homelab has real PVs so the model persists across restarts.

---

## Phase 1: Foundation + Store (Day 1 morning)

**Goal:** SQLite schema up, Store CRUD working, vec0 + FTS5 retrieval arms
functional. This is the highest-risk phase — it validates that
`modernc.org/sqlite/vec` actually works with cosine KNN.

### Build

- **`go.mod`** — module `github.com/clcollins/mnemo`, deps: `modernc.org/sqlite`,
  `modernc.org/sqlite/vec`, `github.com/modelcontextprotocol/go-sdk` v1.6.x,
  `cobra`, `viper`, `google/uuid`
- **`cmd/mnemo/main.go`** — calls `cli.Execute()`
- **`internal/cli/root.go`** — cobra root + viper config binding. Env prefix
  `MNEMO_`. Persistent flags: `--db-path`, `--ollama-host`, `--embed-model`,
  `--embed-dim`, `--log-level`
- **`internal/config/config.go`** — config struct with defaults:
  `DBPath=./mnemo.db`, `OllamaHost=https://ollama.cluster.collins.is`,
  `EmbedModel=nomic-embed-text`, `EmbedDim=768`, `OllamaTimeout=30s`,
  `ChunkTargetTokens=400`, `ChunkOverlap=40`
- **`internal/store/store.go`** — interfaces + types (Chunk, MemoryWrite,
  IngestResult, Ranked, Filter, Status) exactly as in the original plan §4.2
- **`internal/store/sqlite/sqlite.go`** — `New(dbPath, embedDim)`, `Migrate`,
  `Remember`, `Forget`, `Status`, `UpsertChunks`, `Close`
- **`internal/store/sqlite/search.go`** — `VectorSearch` (query chunks_vec,
  over-fetch k*3, join to chunks for filtering), `KeywordSearch` (FTS5 MATCH
  with bm25 ranking)

### Schema

Use the schema from the original plan §5 verbatim — spaces table, chunks
table with indexes, chunks_vec (vec0 float[768] cosine), chunks_fts (FTS5
external-content), sync triggers, query_log, seed spaces.

### Tests first

- `TestMigrate` — schema creates, spaces seeded
- `TestVec0RoundTrip` — insert vector into chunks_vec, KNN query returns it
  (**the critical gate**)
- `TestRememberAndForget` — insert via Remember, Forget sets forgotten=1
- `TestUpsertChunks_Dedup` — same content_hash skipped
- `TestVectorSearch` — 5 seeded chunks, query returns correct ordering
- `TestKeywordSearch` — FTS5 MATCH returns correct hits
- `TestKeywordSearch_SpecialChars` — punctuation doesn't crash FTS5
- `TestFilter_SpaceAndForgotten` — filters respected

### Gate

`go test ./internal/store/sqlite/...` all green. Vec0 KNN works with cosine
distance via `modernc.org/sqlite/vec`.

---

## Phase 2: Embedder + Fusion + Ingestion (Day 1 PM — Day 2 AM)

### 2a: Embedder

- **`internal/embed/embedder.go`** — `Embedder` interface from plan §4.1
- **`internal/embed/fake.go`** — `FakeEmbedder` returning deterministic
  vectors (hash text, use first N floats)
- **`internal/embed/ollama.go`** — `OllamaEmbedder`: POST to
  `{host}/api/embed` with `{"model": m, "input": texts}`, parse `embeddings`
  array. Standard `net/http` with configured timeout. `Validate(ctx)` fetches
  model info and confirms dimension matches config.

Tests: httptest-based unit tests for order preservation, batch, error handling,
dimension mismatch. Build-tagged `//go:build integration` test hitting real
Ollama.

### 2b: Search Fusion

- **`internal/search/fusion.go`** — `HybridSearch(ctx, query, embedder, store,
  filter, limit, maxTokens)`:
  - Embed the raw query (no expansion — spike simplification)
  - `store.VectorSearch(queryVec, filter, limit*3)`
  - Extract key terms (split whitespace, drop stopwords, FTS5-quote each)
  - `store.KeywordSearch(terms, filter, limit*3)`
  - RRF: `score += 1.0/(60+vecRank)` + `score += 0.5/(60+ftsRank)`
  - Importance boost: `+= 0.15 * importance`
  - Recency boost: `+= 0.05 * (1/(1 + ageDays/90))`
  - Sort descending, take top limit
  - Token budget: full content up to 60% of maxTokens, remainder truncated
    to ~200 chars with `[truncated]`

Tests: use FakeEmbedder + in-memory fake Store. Test RRF scoring, both-arm
fusion, importance/recency boosts, token budget truncation.

### 2c: Ingestion Pipeline

- **`internal/ingest/chunker.go`** — `Chunker` interface, `Document` type
- **`internal/ingest/markdown.go`** — `MarkdownChunker`: split on heading
  boundaries, track heading_path, target ~400 tokens, split large sections
  on paragraph boundaries, preserve code fences, sha256 content hash. Strip
  YAML frontmatter.
- **`internal/ingest/pipeline.go`** — `Ingest(ctx, path, embedder, store)`:
  walk path recursively, select chunker by extension (.md → MarkdownChunker,
  .json → chunk by top-level keys with heading_path `"filename > key"`,
  .txt/.html → single chunk per file), batch embed new chunks, upsert
- **`internal/cli/ingest.go`** — cobra `ingest` subcommand

Tests: heading split, heading_path hierarchy, code fence preservation, large
section splitting, frontmatter stripping, content hash determinism, recursive
walk, incremental re-ingest (run twice = 0 new chunks).

### Gate

`go test ./internal/embed/... ./internal/search/... ./internal/ingest/...` all
green. Manual test: `go run ./cmd/mnemo ingest ~/Wiki/` with real Ollama works.

---

## Phase 3: MCP Server + Eval (Day 2)

### 3a: Memory Service

- **`internal/memory/service.go`** — `Service` struct holding Embedder + Store:
  `Recall` (delegates to search.HybridSearch), `Remember` (embed + classify +
  store), `Forget`, `Status`
- **`internal/memory/classify.go`** — keyword classification: decided→decision
  0.8, learned→lesson 0.75, prefer→preference 0.7, pattern→pattern 0.7,
  default→fact 0.5

### 3b: MCP Server

- **`internal/mcp/server.go`** — `New(svc) *mcp.Server` using the official Go
  SDK pattern:

```go
server := mcp.NewServer(&mcp.Implementation{Name: "mnemo"}, nil)
mcp.AddTool(server, &mcp.Tool{Name: "recall", Description: "..."}, recallHandler)
// ... remember, forget, memory_status
```

Four tools with typed input structs (jsonschema tags for descriptions):

- `recall(query, spaces?, limit=10, max_tokens=2000)` → ranked chunks
- `remember(text, space="default", source="mcp", speaker="human")` → stored chunk
- `forget(chunk_id)` → soft delete
- `memory_status()` → health + counts

- **`internal/cli/serve.go`** — cobra `serve` subcommand: construct
  config/embedder/store/service/server, `server.Run(ctx, &mcp.StdioTransport{})`
- **`.mcp.json.example`** — Claude Code config example

Tests: create server with fakes, call tools via SDK in-memory transport
(`mcp.NewInMemoryTransports()`), verify round-trips.

### 3c: CI Scaffolding

- **`Makefile`** — `build`, `test`, `lint`, `clean`, `ci-build`, `ci-checks`,
  `ci-all`, `yaml-lint`, `markdown-lint`, `docs-check`. `CGO_ENABLED=0`.
  `CONTAINER_SUBSYS` variable defaulting to podman.
- **`test/Containerfile.ci`** — golang base + golangci-lint, yamllint,
  markdownlint-cli2, checkmake
- **`Containerfile`** — multi-stage: build with Go, runtime with
  fedora-minimal, OCI labels
- **`.yamllint.yaml`**, **`.markdownlint.yaml`** — standard configs

### 3d: Manual Evaluation

1. Ensure `nomic-embed-text` is available on homelab Ollama
2. `go run ./cmd/mnemo ingest ~/Wiki/ ~/InFlight/Landed/`
3. Verify chunk count, timing, files processed
4. Re-run — verify 0 new chunks (dedup works)
5. Configure `.mcp.json` or test `recall` directly via CLI/script
6. Run 10-15 representative queries, record results:

| Query | Top chunks (source + heading) | Relevant? |
| ----- | ----------------------------- | --------- |
| "How do I migrate OLM to PKO?" | | |
| "What is the Vector log forwarding architecture?" | | |
| "How does configure-alertmanager-operator work?" | | |
| "What are known Konflux pipeline issues?" | | |
| ... | | |

**Success:** majority of queries surface relevant chunks in top 3 results.

### Gate

`go test ./...` all green. `go build ./cmd/mnemo` produces a working binary.
MCP server works with Claude Code over stdio. Retrieval quality is acceptable
for representative queries.

---

## Repository Layout (spike-scoped)

```text
mnemo/
  cmd/mnemo/           main.go
  internal/
    cli/               root.go, serve.go, ingest.go
    config/            config.go
    embed/             embedder.go, ollama.go, fake.go, ollama_test.go
    store/             store.go (interface + types)
      sqlite/          sqlite.go, search.go, sqlite_test.go
    search/            fusion.go, fusion_test.go
    ingest/            chunker.go, markdown.go, pipeline.go, *_test.go
    memory/            service.go, classify.go, service_test.go
    mcp/               server.go, server_test.go
  Makefile
  Containerfile
  test/Containerfile.ci
  .yamllint.yaml
  .markdownlint.yaml
  .mcp.json.example
  go.mod
  docs/plans/
    mnemo-implementation-plan.md   (original, preserved as reference)
    mnemo-spike-plan.md            (this plan, for the spike)
```

---

## Key Technical Decisions

- **sqlite-vec via pure-Go**: `modernc.org/sqlite/vec` blank import, no CGo.
  Published April 2026. If it fails in Phase 1, fallback to
  `mattn/go-sqlite3` + CGo bindings.
- **Ollama `/api/embed`** (not deprecated `/api/embeddings`): batch input,
  returns L2-normalized float32 vectors.
- **vec0 dimension hardcoded to 768**: matches nomic-embed-text. Changing
  model later requires rebuilding the vec table.
- **No query expansion for the spike**: single query vector + single FTS
  query, fused with RRF. Expansion can be added later.
- **JSON chunking**: parse JSON, iterate top-level keys, each key's value
  becomes a chunk with heading_path `"filename > key"`.
- **Logging**: `log/slog` to stderr (stdout is MCP stdio pipe).
- **FTS5 safety**: quote each search term individually (`"term1" "term2"`)
  to prevent syntax errors from special characters.

---

## What's Deferred (not forgotten)

These are explicitly deferred from the spike, not abandoned. The interfaces
(`Embedder`, `Store`, `Chunker`) are designed to accommodate all of them:

- Pi/arm64 cross-compile and testing
- HTTPS CA bundles, auth headers, proxy handling in Embedder
- Query expansion (multiple query variations)
- Shared backend (rqlite/libSQL/Postgres — original plan §9)
- Automated eval harness with recall@k metrics
- Cross-environment parity testing
- Cold-reload retry logic in Embedder
- `--prune` flag for removing stale chunks

---

## Verification

1. `go test ./...` — all unit tests pass (no network required)
2. `go test -tags integration ./...` — Ollama integration tests pass
3. `go build -o /tmp/mnemo ./cmd/mnemo` — binary builds
4. `make ci-checks` — lint, vet, fmt all pass
5. `/tmp/mnemo ingest ~/Wiki/` — ingests corpus, reports chunk counts
6. `/tmp/mnemo ingest ~/Wiki/` (again) — reports 0 new chunks
7. Configure MCP in Claude Code, test `recall` tool interactively
8. Manual eval table completed with acceptable hit rate
