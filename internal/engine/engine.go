// Package engine wires search, synth, and audit into one Ask() call — the
// orchestration layer a CLI or HTTP handler calls, so those entry points
// don't each have to remember to log every query themselves.
package engine

import (
	"context"
	"time"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
	"github.com/Kuwar-Vishnudatt/permrag/internal/audit"
	"github.com/Kuwar-Vishnudatt/permrag/internal/search"
	"github.com/Kuwar-Vishnudatt/permrag/internal/synth"
)

type Engine struct {
	Index *search.Index
	Synth synth.Synthesizer
	Audit *audit.Log
	Now   func() time.Time
}

func New(index *search.Index, s synth.Synthesizer, auditLog *audit.Log) *Engine {
	return &Engine{Index: index, Synth: s, Audit: auditLog, Now: time.Now}
}

// AskResult is what a caller gets back from one query.
type AskResult struct {
	Answer          synth.Answer
	SuppressedCount int
}

const defaultLimit = 8

// Ask runs the full pipeline: ACL-filtered search, synthesis over exactly
// the permitted chunks, and an audit log entry recording what happened —
// in that order, so the audit record reflects the actual counts the
// requester received, not a separately-computed guess.
func (e *Engine) Ask(ctx context.Context, u acl.User, query string) (AskResult, error) {
	sr, err := e.Index.Search(query, u, defaultLimit)
	if err != nil {
		return AskResult{}, err
	}

	answer, err := e.Synth.Synthesize(ctx, query, sr.Results)
	if err != nil {
		return AskResult{}, err
	}

	if e.Audit != nil {
		_ = e.Audit.Record(audit.Entry{
			Time:            e.Now(),
			UserName:        u.Name,
			UserRole:        u.Role,
			UserDepartment:  u.Department,
			Query:           query,
			PermittedCount:  len(sr.Results),
			SuppressedCount: sr.SuppressedCount,
		})
	}

	return AskResult{Answer: answer, SuppressedCount: sr.SuppressedCount}, nil
}
