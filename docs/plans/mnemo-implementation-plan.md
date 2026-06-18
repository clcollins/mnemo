# Implementation Plan: Mnemosyne — a local-first agent memory & retrieval MCP

> **Status:** Draft v1 · **Audience:** implementing coding agent (e.g. Claude Code) · **Author of spec:** generated with Claude Opus 4.8
> **Project:** **Mnemosyne** (Titaness of memory). Binary, identifiers, and env-var prefix: `mnemo` / `MNEMO_`. Suggested module path: `github.com/clcollins/mnemo`.

---

## 1. Problem statement

We have a large, authoritative product corpus. An agent needs reliable knowledge of it that (a) does not get silently dropped when the context window compacts, and (b) does not cost a fortune in tokens by forcing the agent to parse gigantic `.md` files on every session. Today those `.md` files are loaded wholesale into context.

This is fundamentally a **retrieval-augmented generation (RAG)** problem — query a large pre-existing corpus and pull back only the few relevant pieces — with a **secondary episodic-memory** capability (the agent accumulates decisions/preferences/lessons over time). Both use identical retrieval machinery; they differ only in ingestion path and lifecycle, and are separated by a `space` column.

**Explicitly out of scope for v1:** the knowledge-graph / entity-extraction layer that the reference project (Memory Vault) includes but does not expose over MCP. May be added later.

### Success criteria (what the local prototype must prove)
1. Hybrid retrieval over a real slice of the product corpus surfaces the correct chunk(s) for representative questions at a measurably better hit-rate than the agent gets from raw `.md` dumping.
2. Token cost per question drops substantially vs. loading whole files (we retrieve a bounded handful of chunks, budgeted).
3. Re-ingesting changed docs only re-embeds changed chunks (content-hash dedup works).

> **Honest guarantee:** retrievable storage means corpus knowledge can never be *lost* (it lives outside the context window, always fetchable). It does **not** guarantee the agent retrieves the right chunk every time — that depends on chunking and ranking quality, which is exactly what the prototype measures.

---

## 2. Architecture

Claude only ever talks to the MCP server. The server is a hub; Ollama and the database are private helpers it calls. **No ML model lives inside this binary** — embeddings are produced by Ollama over HTTP. Server startup is therefore trivial (open DB, listen).

```
        Claude / agent
            │  MCP tools (text in, text out)
            ▼
   ┌──────────────────────┐
   │   mnemo (Go binary)  │
   │  ┌────────────────┐  │
   │  │ memory.Service │  │   orchestration
   │  └───┬────────┬───┘  │
   │  Embedder    Store   │   ← two interfaces (pluggable)
   └──────┼────────┼──────┘
          │        │
          ▼        ▼
       Ollama    SQLite (sqlite-vec + FTS5)
   (text→vector) (vectors + content + keyword index)
```

The **same binary** ships two cobra subcommands sharing one config + schema:
- `mnemo serve` — the stdio MCP server (read-mostly + episodic writes).
- `mnemo ingest <path>` — the corpus ingestion pipeline (chunk → embed → upsert).

### Lifecycle decision (v1)
**stdio one-shot transport.** The MCP client spawns the binary per session; it exits when the pipe closes. Viable precisely because there is no model to keep warm. This implies **per-machine local SQLite** — fine for the prototype, but see §9 for the shared-backend path, which is the near-term next step for the corpus specifically.

---

## 3. Technology choices

> **Per project policy:** these are the latest stable releases known at spec time. The implementing agent must verify the newest stable version of each at build time and read that version's docs before use. Pin exact versions in `go.mod` (sqlite-vec minor releases may introduce breaking changes).

| Concern | Choice | Notes |
|---|---|---|
| Language / entry point | Go + **cobra/viper** | per project convention |
| MCP framework | **`github.com/modelcontextprotocol/go-sdk`** (official, ≥ v1.2.0) | stdio + Streamable HTTP transports, typed tools via generics |
| Vector search | **`sqlite-vec`** (asg017, ≥ v0.1.9) | `vec0` virtual table, brute-force KNN — ample at personal/team corpus scale |
| Keyword search | **SQLite FTS5** | built in; `bm25()` ranking |
| SQLite driver | **decide via spike (§10, Milestone 0)** | Pref: pure-Go `modernc.org/sqlite` (has sqlite-vec support as of Mar 2026) for trivial ARM cross-compile to Pi nodes & static binary. Fallback: CGo `mattn/go-sqlite3` + `github.com/asg017/sqlite-vec-go-bindings/cgo` if pure-Go vec support proves immature. |
| Embeddings | **Ollama** HTTP (`/api/embeddings`) | default model `nomic-embed-text` (768-dim). Alt `all-minilm` (384-dim, the direct Memory Vault equivalent). Dimension is config and **pinned** — changing it invalidates all stored vectors. |
| Config | **viper** | env + file; 12-factor |
| Container | **Containerfile** | per project convention (not Dockerfile) |
| Build | **Makefile** | per project convention |

---

## 4. Pluggability — the two core interfaces

The two explicitly-requested seams. Keep them small. Fusion/scoring lives in a shared service layer (written and tested once), so a new storage backend only implements low-level CRUD + the two retrieval arms — not the ranking math.

### 4.1 `Embedder` — LLM-provider-agnostic
```go
// internal/embed
type Embedder interface {
    // Embed returns one vector per input text, order-preserving.
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    // Dimensions is the fixed output width; used to validate against stored config.
    Dimensions() int
    // ModelID identifies the model+version, persisted so we can detect
    // an incompatible model swap and refuse / trigger re-embed.
    ModelID() string
}
```
- `OllamaEmbedder` (default): POSTs to `${OLLAMA_HOST}/api/embeddings`. Batches where possible; handles the cold-reload latency on Ollama's first call after idle. **Must be remote-capable, not localhost-only** — this is a hard requirement, since both test environments (§10) point at one shared *remote* endpoint. That means:
  - Configurable base URL over **HTTPS** (not just `http://localhost:11434`); verify the server cert against a configurable CA bundle (`MNEMO_OLLAMA_CA_FILE`) for self-signed/internal CAs.
  - Optional **auth header** (`MNEMO_OLLAMA_AUTH_HEADER` / token) for endpoints gated behind a proxy (e.g. Authentik forward-auth on Envoy Gateway, or a work-internal gateway). Send it on every request.
  - Respect standard **proxy env** (`HTTPS_PROXY`/`NO_PROXY`) — relevant on a corporate work laptop.
  - Sane **timeouts + bounded retry** (the round-trip is now a network hop, possibly over VPN); retry once on Ollama's cold-model reload.
  - On startup, fetch/validate the model's reported dimension against `MNEMO_EMBED_DIM` and persist `ModelID` — so a single shared endpoint guarantees **identical, comparable embeddings across both environments**.
- Future: `OpenAIEmbedder`, etc. — added without touching callers.

### 4.2 `Store` — database-agnostic
```go
// internal/store
type Store interface {
    // CRUD / episodic
    Remember(ctx context.Context, w MemoryWrite) (Chunk, error)
    Forget(ctx context.Context, chunkID string) error
    Status(ctx context.Context) (Status, error)

    // Ingestion (corpus): content-hash upsert, skips unchanged chunks.
    UpsertChunks(ctx context.Context, chunks []Chunk) (IngestResult, error)

    // Retrieval arms — the service layer fuses these (RRF + boosts).
    VectorSearch(ctx context.Context, queryVec []float32, f Filter, k int) ([]Ranked, error)
    KeywordSearch(ctx context.Context, terms string, f Filter, k int) ([]Ranked, error)

    Migrate(ctx context.Context) error
    Close() error
}

type Filter struct {
    Spaces    []string   // e.g. ["product"] or ["session"]
    Since     *time.Time
    NotForgotten bool     // default true
}
```
- `sqlite.Store` (v1): sqlite-vec + FTS5.
- Future: `postgres.Store` (pgvector), `rqlite`/`libsql` clients — see §9. A backend that can fuse in-DB more efficiently MAY additionally implement an optional `HybridSearcher` interface that the service layer prefers when present; otherwise the shared fusion is used.

### 4.3 `Chunker` — content-type-agnostic (ingestion only)
```go
// internal/ingest
type Chunker interface {
    Chunk(doc Document) ([]Chunk, error)
}
```
- `MarkdownChunker` (v1): heading-aware splitting (§7).
- Future: `PlaintextChunker`, code-aware, etc.

---

## 5. Data model (SQLite schema)

One schema, two spaces (`product` for the RAG corpus, `session`/`default` for episodic). Faithful to Memory Vault's chunk model, adapted to SQLite.

```sql
-- spaces: namespacing + lifecycle separation
CREATE TABLE IF NOT EXISTS spaces (
  id          INTEGER PRIMARY KEY,
  name        TEXT UNIQUE NOT NULL,
  description TEXT,
  created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- chunks: the core unit (rich row lives here; vector lives in vec table keyed by rowid)
CREATE TABLE IF NOT EXISTS chunks (
  rowid        INTEGER PRIMARY KEY,           -- joins to vec0 + fts5
  id           TEXT UNIQUE NOT NULL,          -- external UUID
  space_id     INTEGER NOT NULL REFERENCES spaces(id),
  content      TEXT NOT NULL,
  content_hash TEXT NOT NULL,                 -- sha256; dedup + incremental re-ingest
  source       TEXT,                          -- file path / URL / "mcp"
  speaker      TEXT,                           -- human | assistant | null
  heading_path TEXT,                           -- e.g. "Billing > Refunds > Partial"
  category     TEXT,                           -- decision|lesson|preference|pattern|fact|doc
  importance   REAL NOT NULL DEFAULT 0.5,
  forgotten    INTEGER NOT NULL DEFAULT 0,     -- soft delete
  metadata     TEXT NOT NULL DEFAULT '{}',     -- JSON
  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS chunks_space_idx ON chunks(space_id);
CREATE INDEX IF NOT EXISTS chunks_hash_idx  ON chunks(space_id, content_hash);
CREATE INDEX IF NOT EXISTS chunks_forgotten_idx ON chunks(forgotten);

-- vectors (sqlite-vec). DIM is config; pin it.
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_vec USING vec0(
  embedding float[768] distance_metric=cosine
);

-- keyword (FTS5), external-content mirror of chunks.content
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
  content, content='chunks', content_rowid='rowid'
);

-- FTS sync triggers (mirror Memory Vault's tsvector trigger)
CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
  INSERT INTO chunks_fts(rowid, content) VALUES (new.rowid, new.content);
END;
CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
  INSERT INTO chunks_fts(chunks_fts, rowid, content) VALUES('delete', old.rowid, old.content);
END;
CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE OF content ON chunks BEGIN
  INSERT INTO chunks_fts(chunks_fts, rowid, content) VALUES('delete', old.rowid, old.content);
  INSERT INTO chunks_fts(rowid, content) VALUES (new.rowid, new.content);
END;

-- query log (observability; parity with reference)
CREATE TABLE IF NOT EXISTS query_log (
  id           TEXT PRIMARY KEY,
  query_text   TEXT,
  spaces       TEXT,
  result_count INTEGER,
  top_score    REAL,
  latency_ms   INTEGER,
  created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT OR IGNORE INTO spaces(name, description) VALUES
  ('product', 'Authoritative product corpus (RAG, ingested)'),
  ('default', 'Episodic agent memory (accumulated)');
```

> **Note on `vec0` filtering:** KNN in `vec0` can't pre-filter on arbitrary columns. Over-fetch `k*3` from `chunks_vec`, then filter (space / since / not-forgotten) by joining to `chunks` on `rowid` in the outer query — exactly how Memory Vault over-fetches.

### Corpus vs. episodic lifecycle
| | Product corpus (`product`) | Episodic (`default`) |
|---|---|---|
| Written by | `mnemo ingest` pipeline | agent via `remember` |
| `forget` semantics | re-ingest; don't soft-delete | soft-delete is meaningful |
| Volatility | changes when docs change | grows continuously |

---

## 6. Retrieval: hybrid search + fusion

Port Memory Vault's algorithm faithfully; keep the constants. Lives in `internal/search`, operates on `Store`'s two arms — so it's testable against a fake store with no DB.

1. **Query expansion** (`expand.go`): up to 3 variations — original, keyword-extracted, and a question→statement "broad" form. (Reference uses the embedding model's WordPiece tokenizer; for a provider-agnostic design, use a simple stopword-based key-term extractor — keep it behind a function so it can be upgraded.)
2. **Arm 1 — vector:** embed each variation, KNN each (`k*3`), union + dedup by chunk, keep best similarity, rank.
3. **Arm 2 — keyword:** build a safe FTS5 `MATCH` string from extracted terms (phrase-quote each term to avoid FTS5 syntax errors on punctuation), `bm25()` rank.
4. **Fuse — Reciprocal Rank Fusion**, then boosts:
   - `RRF_K = 60`
   - `FTS_WEIGHT = 0.5` → `score += FTS_WEIGHT / (RRF_K + fts_rank)`; vector arm `score += 1 / (RRF_K + vec_rank)`
   - `IMPORTANCE_WEIGHT = 0.15` → `score += 0.15 * importance`
   - recency: `RECENCY_HALF_LIFE_DAYS = 90`, `RECENCY_MAX_BOOST = 0.05` → `score += 0.05 * (1 / (1 + age_days/90))`
5. **Token budget** (`recall` only): top results full content up to 60% of `max_tokens`; remainder truncated to ~200 chars with a `[truncated]` marker; estimate tokens as `len(text)/4`.

---

## 7. Ingestion pipeline (`mnemo ingest <path>`)

The load-bearing new component, separate from the MCP surface. This is the fix for "agent parses giant `.md` files": the pipeline pre-digests them **once** into chunks; the agent thereafter only ever sees the handful it retrieves.

1. **Walk** the path for supported docs (`.md` first).
2. **Chunk** (`MarkdownChunker`): split on heading boundaries into retrieval-sized pieces (target a few hundred tokens; configurable `--target-tokens`, `--overlap`). Preserve the full heading path into `heading_path` so a retrieved chunk is self-locating. Avoid splitting code fences mid-block.
3. **Hash** each chunk (sha256 of normalized content). Skip chunks whose `(space, content_hash)` already exists — incremental re-ingest only embeds changed sections.
4. **Embed** new/changed chunks via `Embedder` (batched).
5. **Upsert** via `Store.UpsertChunks` into `space='product'`, `category='doc'`.
6. **Report**: chunks added / skipped / updated / removed.

Idempotent: re-running on unchanged docs is a no-op (modulo removals). Deletions: chunks whose source file/section no longer produces a matching hash should be pruned (flag-gated: `--prune`).

---

## 8. MCP surface

Tool descriptions do most of the agent-instruction work — write them carefully (the reference's docstrings are the model here).

**Tools**
- `recall(query, spaces?, since?, limit=10, max_tokens=2000)` → ranked chunks (hybrid + budgeted). `spaces` defaults to all; agents querying product knowledge pass `["product"]`.
- `remember(text, space="default", source="mcp", speaker="human")` → embeds, sha256-dedup, auto-classify category+importance, insert. (Episodic; not the corpus path.)
- `forget(chunk_id)` → soft delete (importance→0, `forgotten=1`).
- `memory_status()` → health, per-space counts, model id, query stats.

**Resources**
- `memory://spaces`, `memory://stats`.

**Classification heuristics** (`classify.go`, ported): decision 0.8 / lesson 0.75 / preference 0.7 / pattern 0.7 / fact 0.5, by keyword match; default fact.

**Agent instruction snippet** (for `CLAUDE.md`, ship in README):
> Persistent memory & product knowledge are available via `recall`, `remember`, `forget`. Call `recall` with `spaces:["product"]` before answering questions about the product instead of assuming context. Call `recall` (all spaces) at task start to restore prior decisions. When a decision is made or something is learned, `remember` it as one concise standalone statement. Use `forget` only when information is explicitly superseded.

---

## 9. Local → shared path (post-prototype)

The corpus is identical for everyone, so per-machine silos mean every developer/CI agent re-ingests and re-embeds the whole corpus and drifts out of sync. The corpus wants a **single shared, read-mostly store** sooner than episodic memory does. Because it is read-mostly (many readers, one ingestion writer), SQLite's single-writer limit is not a problem.

The binary does **not** change between local and shared — only *what it opens* and *how it's launched*. Keep `Store` construction and transport behind config so the swap is config-only:

- **Shared option A (smallest step):** persistent **Streamable HTTP** MCP pod owning one SQLite on a PVC, fronted by Envoy Gateway + Authentik OIDC. Every host's agent talks to the same server. (The official Go MCP SDK supports this transport directly.)
- **Shared option B (networked DB):** `rqlite` (Raft-HA, HTTP API, loads `sqlite-vec` at launch) or `libSQL`/`sqld` (server mode, native vector type) as a `Store` implementation; MCP stays stdio per host but all point at the same DB.
- **Postgres/pgvector:** only if a need arises that SQLite-family can't meet — likely unnecessary; a shared SQLite-family backend should cover it.

Single-writer becomes real only when multiple agents *write* the same store (episodic across developers); rqlite/libSQL/Postgres serialize that for you.

---

## 10. Build plan (TDD, milestone-gated)

Test-driven throughout: write the test first, then the implementation. Unit tests use fakes (`fakeEmbedder` returning deterministic vectors; `fakeStore` for service/fusion tests). Integration tests against a real temp SQLite file and a real Ollama are build-tagged (`//go:build integration`) so the default `go test ./...` stays hermetic and fast.

### 10.0 Test environments & device matrix

The prototype must validate on **two client environments that share one remote Ollama endpoint**. Sharing the endpoint is deliberate: same model + dimension everywhere means embeddings are identical and comparable across machines, which both simplifies parity testing now and de-risks the shared-backend step later (§9).

| | **Env A — Homelab Pi** | **Env B — Work Fedora laptop** |
|---|---|---|
| Host arch | arm64 (Raspberry Pi nodes) | amd64, inside **Fedora Toolbx** container |
| Binary delivery | cross-compiled to arm64 (Milestone 0) | native `go build` in the toolbox, or pulled image |
| SQLite file | local path on the Pi | local path inside `$HOME` (toolbox mounts home by default) |
| Ollama | **remote, shared endpoint** (HTTPS, possibly Authentik-gated) | **same remote endpoint** — reached from inside the toolbox |
| Transport | stdio | stdio |

**Per-environment concerns the tests/runbook must cover:**

- **Shared remote Ollama (both):** the design's `Embedder` is the thing under test here — HTTPS base URL, CA trust, optional auth header, timeouts/retry over a real network hop. The *same* `MNEMO_OLLAMA_*` config should work from both hosts; only the SQLite path differs. Assert the model's reported dimension matches `MNEMO_EMBED_DIM` from both.
- **Pi (Env A):** confirm the cross-compiled arm64 binary runs; watch memory/latency on Pi-class hardware (the binary is tiny since no model is resident, but SQLite + sqlite-vec brute-force KNN scales with corpus size — measure at realistic corpus size). Confirm `sqlite-vec`/driver works on arm64 (ties to Milestone 0).
- **Toolbx (Env B):** toolbox containers share the host network namespace, so the remote endpoint is reachable exactly as on the host — but verify: (1) **corporate proxy/VPN** — `HTTPS_PROXY`/`NO_PROXY` honored, endpoint reachable from the work network; (2) **TLS/CA** — if the endpoint uses an internal CA, the CA file is visible inside the toolbox (mount/copy into the container's trust store or point `MNEMO_OLLAMA_CA_FILE` at it); (3) **SELinux** — if the SQLite file or CA is bind-mounted from outside `$HOME`, relabel with `:z`/`:Z`; (4) the binary and any CGo deps resolve inside the toolbox base image.
- **Parity:** the same eval set, run in both environments against the same Ollama, must produce the same retrieval ranking (modulo the separate local DBs). This is the explicit cross-environment assertion (Milestone 9).

- **Milestone 0 — spike (de-risk first):** prove the chosen SQLite driver loads `sqlite-vec`, creates a `vec0` table, and runs a KNN query. Decide pure-Go vs CGo here. **Validate the build/run on both targets:** cross-compile to **arm64** and run the `vec0` round-trip on a **Pi**, and build+run natively inside a **Fedora Toolbx** container on amd64. (Pure-Go `modernc.org/sqlite` makes the arm64 cross-compile trivial and CGo-free; if the CGo fallback is needed, confirm the toolbox base image and the Pi both have the toolchain.) *Test:* a `vec0` round-trip returns the expected nearest neighbor on each arch.
- **Milestone 1 — schema + Store CRUD:** `sqlite.Store` migrate, `Remember`/`Forget`/`Status`, content-hash dedup. *Tests:* insert/dedup/soft-delete behavior against temp DB.
- **Milestone 2 — retrieval arms:** `VectorSearch`, `KeywordSearch` (FTS5 `MATCH` builder with escaping). *Tests:* seeded fixtures return expected rows/order; malformed query strings don't error.
- **Milestone 3 — fusion + expansion:** RRF + boosts + token budget, against `fakeStore`. *Tests:* known rank inputs produce expected fused order; recency/importance boosts move items as specified; budget truncates correctly.
- **Milestone 4 — Embedder:** `OllamaEmbedder` (unit with httptest fake; integration tagged). *Tests:* dimension/model-id validation; batch order preserved; cold-reload retry. **Remote-endpoint integration:** point at the shared **remote** Ollama over HTTPS from both a host shell and from inside the Toolbx container; exercise CA trust, auth header, and proxy handling. *Assert:* identical model id + dimension reported from both environments.
- **Milestone 5 — memory.Service:** wire Embedder+Store+search into `recall`/`remember`/`forget`/`status`; classification. *Tests:* end-to-end with fakes.
- **Milestone 6 — MCP server (`serve`, stdio):** map tools/resources to the service; tool descriptions. *Tests:* MCP tool round-trips via the SDK's in-memory transport.
- **Milestone 7 — ingestion (`ingest`):** `MarkdownChunker` + pipeline + incremental re-ingest + `--prune`. *Tests:* chunk boundaries, heading_path, hash-skip on re-run, prune on removal.
- **Milestone 8 — validation harness:** a small eval set of product questions with expected source chunks; report hit-rate@k and token cost vs. raw-file baseline (the success criteria in §1).
- **Milestone 9 — cross-environment parity:** ingest the same corpus slice and run the Milestone 8 eval set in **both** Env A (Pi) and Env B (Toolbx) against the **same remote Ollama**. *Assert:* embeddings and retrieval ranking match across environments (the DBs are separate but content-identical); record latency on each so Pi-class performance is a known quantity. This is the gate that says "validated for homelab and work."

Cross-cutting: `make test lint`, CI mirrors existing repo conventions, `golangci-lint`.

---

## 11. Configuration (viper)

Env (and/or file), all overridable:
```
MNEMO_DB_PATH         ./mnemo.db
MNEMO_OLLAMA_HOST     https://ollama.internal.example   # remote by default for the POC
MNEMO_OLLAMA_CA_FILE                                    # path to internal CA bundle (optional)
MNEMO_OLLAMA_AUTH_HEADER                                # e.g. "Authorization: Bearer …" (optional)
MNEMO_OLLAMA_TIMEOUT  30s
MNEMO_EMBED_MODEL     nomic-embed-text
MNEMO_EMBED_DIM       768            # MUST match model; pinned
MNEMO_TRANSPORT       stdio          # stdio | http (future)
MNEMO_HTTP_ADDR       :8080          # when transport=http
MNEMO_LOG_LEVEL       info
# ingest
MNEMO_CHUNK_TARGET_TOKENS  400
MNEMO_CHUNK_OVERLAP        40
# standard proxy env (HTTPS_PROXY / NO_PROXY) is honored for the Ollama call — relevant on work laptop
```
On startup, validate stored vectors' `ModelID`/dimension against current config; refuse to mix incompatible embeddings (or require `--reembed`). The same `MNEMO_OLLAMA_*` values are intended to work unchanged from both the Pi and the Toolbx container — only `MNEMO_DB_PATH` differs per host.

---

## 12. Repository layout
```
mnemo/
  cmd/                 root.go (cobra+viper), serve.go, ingest.go, status.go
  internal/
    config/            config.go
    embed/             embedder.go, ollama.go, fake.go (+ _test.go)
    store/             store.go (interface+types)
      sqlite/          sqlite.go, schema.go, fts.go (+ _test.go)
    search/            fusion.go, expand.go (+ _test.go)
    ingest/            chunker.go, markdown.go, pipeline.go (+ _test.go)
    memory/            service.go, classify.go (+ _test.go)
    mcp/               server.go (+ _test.go)
  migrations/          (or embedded in store/sqlite/schema.go)
  Containerfile
  Makefile
  .mcp.json.example
  README.md            (setup, CLAUDE.md snippet, Ollama model pull)
  go.mod
```

---

## 13. Attribution & sources (per project policy)

**Commits** must include:
```
Co-Authored-By: Claude <<MODEL_USED>@noreply.anthropic.com>
```
(replace `<MODEL_USED>` with the actual implementing model, e.g. `Sonnet 4.6`).

**PRs** must include the footer:
```
🤖 Generated with [Claude Code](https://claude.com/claude-code)
```

**Sources that informed this design:**
- Yadullah Abidi, "I fixed Claude's memory problem with a Postgres database…", MakeUseOf, 2026-06-16 (the prompting article).
- `mihaibuilds/memory-vault` — reference implementation (MCP tool contract, hybrid-search + RRF algorithm, classification heuristics, schema model) — read directly from source.
- `asg017/sqlite-vec` — `vec0` virtual table, KNN, rqlite/extension docs.
- SQLite FTS5 documentation — `bm25()` ranking, external-content tables.
- `modelcontextprotocol/go-sdk` (official Go MCP SDK) — transports, typed tools.
- `tursodatabase/libsql` and rqlite docs — shared/networked SQLite path (§9).

---

## 14. Open decisions for the implementer
1. SQLite driver: confirm pure-Go `modernc.org/sqlite` vec support is production-viable (Milestone 0); else CGo fallback.
2. Embedding model/dimension default: `nomic-embed-text`/768 vs `all-minilm`/384 — pick based on Milestone 8 hit-rate vs. storage/latency on Pi-class hardware.
3. Chunk sizing defaults — tune against the real corpus in Milestone 8.
4. Whether episodic memory stays per-machine while corpus goes shared first (§9), or both move together.
5. Whether the work POC reaches the homelab Ollama or a work-hosted one — either way the `Embedder` contract is identical (remote HTTPS, optional auth/CA/proxy); confirm which, and whether corporate network policy permits the egress, before Milestone 4.
