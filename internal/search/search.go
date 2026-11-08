// Package search is a real SQLite FTS5-backed index where access control
// is enforced INSIDE the SQL query itself, not as a filter applied to
// results after the fact. That distinction is the entire security
// guarantee this package exists to provide: a restricted chunk a user
// isn't allowed to see is never returned as a row at all, so there's no
// downstream code path — answer synthesis, logging, a future feature
// someone adds later — that could accidentally forget to filter it out.
package search

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
	"github.com/Kuwar-Vishnudatt/permrag/internal/docstore"
)

type Index struct {
	db *sql.DB
}

func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	schema := `
	CREATE TABLE IF NOT EXISTS chunks_meta (
		chunk_id TEXT PRIMARY KEY,
		doc_id TEXT NOT NULL,
		doc_title TEXT NOT NULL,
		min_role_rank INTEGER NOT NULL,
		dept_restricted INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS chunk_departments (
		chunk_id TEXT NOT NULL,
		department TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_chunk_departments ON chunk_departments(chunk_id);
	CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(chunk_id UNINDEXED, body);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Index{db: db}, nil
}

func (ix *Index) Close() error { return ix.db.Close() }

// Add indexes one chunk, recording its policy as queryable metadata:
// min_role_rank (an integer, so SQL can compare it directly) and the
// explicit department list in a proper join table rather than a
// delimited string column — a LIKE-based match on a comma-joined column
// is exactly the kind of thing that would wrongly match "eng" against
// "engineering-finance" or similar; an exact-match join table has no such
// failure mode.
func (ix *Index) Add(c docstore.Chunk) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	deptRestricted := 0
	if len(c.Policy.Departments) > 0 {
		deptRestricted = 1
	}

	_, err = tx.Exec(
		`INSERT OR REPLACE INTO chunks_meta (chunk_id, doc_id, doc_title, min_role_rank, dept_restricted) VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.DocID, c.DocTitle, acl.Rank(c.Policy.MinRole), deptRestricted,
	)
	if err != nil {
		return fmt.Errorf("insert meta: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM chunk_departments WHERE chunk_id = ?`, c.ID); err != nil {
		return err
	}
	for _, d := range c.Policy.Departments {
		if _, err := tx.Exec(`INSERT INTO chunk_departments (chunk_id, department) VALUES (?, ?)`, c.ID, d); err != nil {
			return fmt.Errorf("insert department: %w", err)
		}
	}

	if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE chunk_id = ?`, c.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO chunks_fts (chunk_id, body) VALUES (?, ?)`, c.ID, c.Text); err != nil {
		return fmt.Errorf("insert fts: %w", err)
	}

	return tx.Commit()
}

// Result is one permitted search hit.
type Result struct {
	ChunkID  string
	DocID    string
	DocTitle string
	Text     string
	Score    float64 // FTS5 bm25: MORE NEGATIVE is a better match
}

// SearchResult is the outcome of one query: the permitted hits, plus a
// count (never titles or content — see package doc) of additionally
// relevant chunks that exist but were withheld by the ACL filter.
type SearchResult struct {
	Results         []Result
	SuppressedCount int
}

// Search runs an FTS5 MATCH query filtered to exactly what u is allowed to
// see, entirely inside the WHERE clause, and separately counts (but never
// returns the content or titles of) additional matches that exist only for
// higher-privileged users — useful operator/UX signal ("3 more relevant
// documents require manager-level access") without leaking anything about
// what those documents actually contain.
func (ix *Index) Search(query string, u acl.User, limit int) (SearchResult, error) {
	ftsQuery := sanitizeFTSQuery(query)
	if ftsQuery == "" {
		return SearchResult{}, nil
	}

	crossDept := 0
	if acl.IsCrossDepartmental(u.Role) {
		crossDept = 1
	}

	rows, err := ix.db.Query(`
		SELECT DISTINCT m.chunk_id, m.doc_id, m.doc_title, f.body, bm25(chunks_fts) AS score
		FROM chunks_fts f
		JOIN chunks_meta m ON m.chunk_id = f.chunk_id
		WHERE chunks_fts MATCH ?
		  AND m.min_role_rank <= ?
		  AND (
		        m.dept_restricted = 0
		        OR ? = 1
		        OR EXISTS (SELECT 1 FROM chunk_departments cd WHERE cd.chunk_id = m.chunk_id AND cd.department = ?)
		      )
		ORDER BY score ASC
		LIMIT ?
	`, ftsQuery, acl.Rank(u.Role), crossDept, u.Department, limit)
	if err != nil {
		return SearchResult{}, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []Result
	permitted := map[string]bool{}
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ChunkID, &r.DocID, &r.DocTitle, &r.Text, &r.Score); err != nil {
			return SearchResult{}, err
		}
		results = append(results, r)
		permitted[r.ChunkID] = true
	}
	if err := rows.Err(); err != nil {
		return SearchResult{}, err
	}

	suppressed, err := ix.countSuppressed(ftsQuery, permitted)
	if err != nil {
		return SearchResult{}, err
	}

	return SearchResult{Results: results, SuppressedCount: suppressed}, nil
}

// countSuppressed re-runs the same MATCH with no ACL filter at all, then
// counts how many of those chunk IDs are NOT in the permitted set. This is
// the only place the unrestricted result set is ever materialized, and it
// is used solely to produce a count — never to read back doc_title or
// body, which is what keeps this feature from becoming its own leak.
func (ix *Index) countSuppressed(ftsQuery string, permitted map[string]bool) (int, error) {
	rows, err := ix.db.Query(`SELECT chunk_id FROM chunks_fts WHERE chunks_fts MATCH ?`, ftsQuery)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		if !permitted[id] {
			count++
		}
	}
	return count, rows.Err()
}

// sanitizeFTSQuery builds a plain OR-of-terms FTS5 query from free text,
// escaping each term as an FTS5 string literal. Without this, a query
// containing FTS5 syntax characters (quotes, colons, parentheses — all of
// which a real user's search text can contain incidentally, e.g. a query
// about a document literally named "Section 3: Benefits") would either
// throw a syntax error or, worse, be interpreted as FTS5 query syntax the
// user didn't intend.
func sanitizeFTSQuery(query string) string {
	fields := strings.Fields(query)
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		cleaned := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, f)
		if cleaned != "" {
			terms = append(terms, fmt.Sprintf(`"%s"`, cleaned))
		}
	}
	return strings.Join(terms, " OR ")
}
