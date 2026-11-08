// Package audit records every query against this system — who asked,
// what was permitted, how much was suppressed — and gates who may REVIEW
// that log. An audit trail that anyone can read is itself a data-quality
// and privacy problem (it would show exactly who asked about what, which
// is sensitive in its own right for an HR system); review access is
// restricted to HR admin and exec, enforced the same way search results
// are: a request from an unauthorized role simply returns no rows rather
// than relying on a caller to check before displaying anything.
package audit

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
)

type Log struct {
	db *sql.DB
}

func Open(path string) (*Log, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	schema := `
	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts TEXT NOT NULL,
		user_name TEXT NOT NULL,
		user_role TEXT NOT NULL,
		user_department TEXT NOT NULL,
		query TEXT NOT NULL,
		permitted_count INTEGER NOT NULL,
		suppressed_count INTEGER NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Log{db: db}, nil
}

func (l *Log) Close() error { return l.db.Close() }

// Entry is one recorded query.
type Entry struct {
	Time            time.Time
	UserName        string
	UserRole        acl.Role
	UserDepartment  string
	Query           string
	PermittedCount  int
	SuppressedCount int
}

func (l *Log) Record(e Entry) error {
	_, err := l.db.Exec(
		`INSERT INTO audit_log (ts, user_name, user_role, user_department, query, permitted_count, suppressed_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Time.UTC().Format(time.RFC3339Nano), e.UserName, string(e.UserRole), e.UserDepartment,
		e.Query, e.PermittedCount, e.SuppressedCount,
	)
	return err
}

// Review returns every recorded entry, but ONLY if requester has
// sufficient privilege (hr_admin or exec). Any other role gets an empty
// slice and no error — mirroring package search's "unauthorized data is
// never materialized, not just hidden after the fact" discipline, so a
// future caller that forgets to check a boolean can't accidentally render
// the whole log to the wrong person.
func (l *Log) Review(requester acl.User) ([]Entry, error) {
	if !acl.IsCrossDepartmental(requester.Role) {
		return nil, nil
	}

	rows, err := l.db.Query(`SELECT ts, user_name, user_role, user_department, query, permitted_count, suppressed_count FROM audit_log ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var tsStr, role string
		if err := rows.Scan(&tsStr, &e.UserName, &role, &e.UserDepartment, &e.Query, &e.PermittedCount, &e.SuppressedCount); err != nil {
			return nil, err
		}
		e.UserRole = acl.Role(role)
		e.Time, err = time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			return nil, fmt.Errorf("parse timestamp: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
