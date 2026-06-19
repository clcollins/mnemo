package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/clcollins/mnemo/internal/store"
	"github.com/google/uuid"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

type Store struct {
	db       *sql.DB
	embedDim int
}

func New(dbPath string, embedDim int) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Store{db: db, embedDim: embedDim}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	schema := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS spaces (
			id          INTEGER PRIMARY KEY,
			name        TEXT UNIQUE NOT NULL,
			description TEXT,
			created_at  TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS chunks (
			rowid        INTEGER PRIMARY KEY,
			id           TEXT UNIQUE NOT NULL,
			space_id     INTEGER NOT NULL REFERENCES spaces(id),
			content      TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			source       TEXT,
			speaker      TEXT,
			heading_path TEXT,
			category     TEXT,
			importance   REAL NOT NULL DEFAULT 0.5,
			forgotten    INTEGER NOT NULL DEFAULT 0,
			metadata     TEXT NOT NULL DEFAULT '{}',
			created_at   TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE INDEX IF NOT EXISTS chunks_space_idx ON chunks(space_id);
		CREATE INDEX IF NOT EXISTS chunks_hash_idx  ON chunks(space_id, content_hash);
		CREATE INDEX IF NOT EXISTS chunks_forgotten_idx ON chunks(forgotten);

		CREATE VIRTUAL TABLE IF NOT EXISTS chunks_vec USING vec0(
			embedding float[%d] distance_metric=cosine
		);

		CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
			content, content='chunks', content_rowid='rowid'
		);

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
	`, s.embedDim)

	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func (s *Store) resolveSpaceID(ctx context.Context, spaceName string) (int64, error) {
	if spaceName == "" {
		spaceName = "default"
	}
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM spaces WHERE name = ?", spaceName).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("resolve space %q: %w", spaceName, err)
	}
	return id, nil
}

func contentHash(content string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return fmt.Sprintf("%x", h)
}

func vecToJSON(v []float32) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Store) Remember(ctx context.Context, w store.MemoryWrite) (store.Chunk, error) {
	spaceID, err := s.resolveSpaceID(ctx, w.Space)
	if err != nil {
		return store.Chunk{}, err
	}

	chunkID := uuid.New().String()
	hash := contentHash(w.Content)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Chunk{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO chunks (id, space_id, content, content_hash, source, speaker, category, importance, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, '{}')`,
		chunkID, spaceID, w.Content, hash, w.Source, w.Speaker, w.Category, w.Importance,
	)
	if err != nil {
		return store.Chunk{}, fmt.Errorf("insert chunk: %w", err)
	}

	rowID, err := res.LastInsertId()
	if err != nil {
		return store.Chunk{}, fmt.Errorf("last insert id: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"INSERT INTO chunks_vec(rowid, embedding) VALUES (?, ?)",
		rowID, vecToJSON(w.Embedding),
	)
	if err != nil {
		return store.Chunk{}, fmt.Errorf("insert vec: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return store.Chunk{}, fmt.Errorf("commit: %w", err)
	}

	return store.Chunk{
		RowID:       rowID,
		ID:          chunkID,
		SpaceID:     spaceID,
		Content:     w.Content,
		ContentHash: hash,
		Source:      w.Source,
		Speaker:     w.Speaker,
		Category:    w.Category,
		Importance:  w.Importance,
		Embedding:   w.Embedding,
	}, nil
}

func (s *Store) Forget(ctx context.Context, chunkID string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE chunks SET forgotten = 1, importance = 0, updated_at = datetime('now') WHERE id = ?",
		chunkID,
	)
	if err != nil {
		return fmt.Errorf("forget: %w", err)
	}
	return nil
}

func (s *Store) Status(ctx context.Context) (store.Status, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.name, COUNT(c.rowid)
		 FROM spaces s
		 LEFT JOIN chunks c ON c.space_id = s.id AND c.forgotten = 0
		 GROUP BY s.id, s.name`)
	if err != nil {
		return store.Status{}, fmt.Errorf("status query: %w", err)
	}
	defer rows.Close()

	status := store.Status{SpaceCounts: make(map[string]int)}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return store.Status{}, fmt.Errorf("scan: %w", err)
		}
		status.SpaceCounts[name] = count
		status.TotalChunks += count
	}
	return status, rows.Err()
}

func (s *Store) UpsertChunks(ctx context.Context, chunks []store.Chunk) (store.IngestResult, error) {
	var result store.IngestResult

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, c := range chunks {
		var exists int
		err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM chunks WHERE space_id = ? AND content_hash = ?",
			c.SpaceID, c.ContentHash,
		).Scan(&exists)
		if err != nil {
			return result, fmt.Errorf("check existing: %w", err)
		}
		if exists > 0 {
			result.ChunksSkipped++
			continue
		}

		chunkID := c.ID
		if chunkID == "" {
			chunkID = uuid.New().String()
		}

		res, err := tx.ExecContext(ctx,
			`INSERT INTO chunks (id, space_id, content, content_hash, source, heading_path, category, importance, metadata)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, '{}')`,
			chunkID, c.SpaceID, c.Content, c.ContentHash, c.Source, c.HeadingPath, c.Category, c.Importance,
		)
		if err != nil {
			return result, fmt.Errorf("insert chunk: %w", err)
		}

		rowID, err := res.LastInsertId()
		if err != nil {
			return result, fmt.Errorf("last insert id: %w", err)
		}

		_, err = tx.ExecContext(ctx,
			"INSERT INTO chunks_vec(rowid, embedding) VALUES (?, ?)",
			rowID, vecToJSON(c.Embedding),
		)
		if err != nil {
			return result, fmt.Errorf("insert vec: %w", err)
		}

		result.ChunksAdded++
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit: %w", err)
	}
	return result, nil
}

func buildFilterClause(f store.Filter, spaceMap map[string]int64) (string, []any) {
	var clauses []string
	var args []any

	if f.NotForgotten {
		clauses = append(clauses, "c.forgotten = 0")
	}
	if len(f.Spaces) > 0 {
		placeholders := make([]string, len(f.Spaces))
		for i, sp := range f.Spaces {
			placeholders[i] = "?"
			args = append(args, spaceMap[sp])
		}
		clauses = append(clauses, "c.space_id IN ("+strings.Join(placeholders, ",")+")") //nolint:gocritic
	}
	if f.Since != nil {
		clauses = append(clauses, "c.created_at >= ?")
		args = append(args, f.Since.UTC().Format(time.DateTime))
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

func (s *Store) loadSpaceMap(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name FROM spaces")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]int64)
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		m[name] = id
	}
	return m, rows.Err()
}

func (s *Store) VectorSearch(ctx context.Context, queryVec []float32, f store.Filter, k int) ([]store.Ranked, error) {
	spaceMap, err := s.loadSpaceMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("load space map: %w", err)
	}

	filterSQL, filterArgs := buildFilterClause(f, spaceMap)
	overFetch := k * 3

	// vec0 KNN can't pre-filter on arbitrary columns, so we over-fetch
	// from the vec table and post-filter by joining to chunks.
	query := fmt.Sprintf(`
		SELECT c.rowid, c.id, c.space_id, c.content, c.content_hash, c.source, c.speaker,
		       c.heading_path, c.category, c.importance, c.forgotten, c.metadata,
		       c.created_at, c.updated_at, v.distance
		FROM (
			SELECT rowid, distance
			FROM chunks_vec
			WHERE embedding MATCH ?
			AND k = ?
		) v
		JOIN chunks c ON c.rowid = v.rowid
		WHERE 1=1
		%s
		ORDER BY v.distance ASC
		LIMIT ?
	`, filterSQL)

	args := []any{vecToJSON(queryVec), overFetch}
	args = append(args, filterArgs...)
	args = append(args, k)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	return scanRankedRows(rows)
}

func (s *Store) KeywordSearch(ctx context.Context, terms string, f store.Filter, k int) ([]store.Ranked, error) {
	if strings.TrimSpace(terms) == "" {
		return nil, nil
	}

	spaceMap, err := s.loadSpaceMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("load space map: %w", err)
	}

	filterSQL, filterArgs := buildFilterClause(f, spaceMap)

	query := fmt.Sprintf(`
		SELECT c.rowid, c.id, c.space_id, c.content, c.content_hash, c.source, c.speaker,
		       c.heading_path, c.category, c.importance, c.forgotten, c.metadata,
		       c.created_at, c.updated_at, bm25(chunks_fts) as score
		FROM chunks_fts fts
		JOIN chunks c ON c.rowid = fts.rowid
		WHERE chunks_fts MATCH ?
		%s
		ORDER BY score ASC
		LIMIT ?
	`, filterSQL)

	args := []any{terms}
	args = append(args, filterArgs...)
	args = append(args, k)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}
	defer rows.Close()

	return scanRankedRows(rows)
}

func scanRankedRows(rows *sql.Rows) ([]store.Ranked, error) {
	var results []store.Ranked
	rank := 1
	for rows.Next() {
		var r store.Ranked
		var createdAt, updatedAt string
		var forgotten int
		var source, speaker, headingPath, category sql.NullString
		err := rows.Scan(
			&r.Chunk.RowID, &r.Chunk.ID, &r.Chunk.SpaceID, &r.Chunk.Content,
			&r.Chunk.ContentHash, &source, &speaker,
			&headingPath, &category, &r.Chunk.Importance,
			&forgotten, &r.Chunk.Metadata,
			&createdAt, &updatedAt, &r.Distance,
		)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		r.Chunk.Source = source.String
		r.Chunk.Speaker = speaker.String
		r.Chunk.HeadingPath = headingPath.String
		r.Chunk.Category = category.String
		r.Chunk.Forgotten = forgotten != 0
		r.Chunk.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
		r.Chunk.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt)
		r.VecRank = rank
		rank++
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *Store) Close() error {
	return s.db.Close()
}
