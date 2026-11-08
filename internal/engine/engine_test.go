package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
	"github.com/Kuwar-Vishnudatt/permrag/internal/audit"
	"github.com/Kuwar-Vishnudatt/permrag/internal/docstore"
	"github.com/Kuwar-Vishnudatt/permrag/internal/search"
	"github.com/Kuwar-Vishnudatt/permrag/internal/synth"
)

func setup(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()
	ix, err := search.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })

	al, err := audit.Open(filepath.Join(dir, "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { al.Close() })

	docs := []docstore.Document{
		{ID: "pub1", Title: "Office Hours", Body: "Office hours are nine to five Monday through Friday.", Policy: acl.Policy{}},
		{ID: "sec1", Title: "Executive Compensation", Body: "Exec compensation packages include confidential equity grants vesting over four years.",
			Policy: acl.Policy{MinRole: acl.RoleExec}},
	}
	for _, d := range docs {
		for _, c := range docstore.ChunkDocument(d) {
			if err := ix.Add(c); err != nil {
				t.Fatal(err)
			}
		}
	}

	e := New(ix, synth.Fake{}, al)
	e.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return e
}

func TestAskReturnsAnswerForPermittedContent(t *testing.T) {
	e := setup(t)
	res, err := e.Ask(context.Background(), acl.User{Name: "alice", Role: acl.RoleEmployee, Department: "sales"}, "office hours")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Answer.Text, "nine to five") {
		t.Fatalf("expected the answer to draw on the public doc, got %q", res.Answer.Text)
	}
}

func TestAskNeverLeaksRestrictedContentToUnauthorizedUser(t *testing.T) {
	e := setup(t)
	// "vesting over four years" appears only in the restricted document's
	// body, never in the query itself — unlike a word such as "equity"
	// that the query also contains, which would make a naive substring
	// check pass even if the leak check were broken, since the Fake's
	// own "no results" message echoes the query text back.
	res, err := e.Ask(context.Background(), acl.User{Name: "eve", Role: acl.RoleEmployee, Department: "sales"}, "executive compensation equity grants")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Answer.Text, "vesting over four years") || strings.Contains(res.Answer.Text, "confidential") {
		t.Fatalf("SECURITY FAILURE: restricted content leaked into an unauthorized answer: %q", res.Answer.Text)
	}
	if res.SuppressedCount != 1 {
		t.Fatalf("expected suppressed count to reflect the withheld exec document, got %d", res.SuppressedCount)
	}
	for _, s := range res.Answer.Sources {
		if s.DocTitle == "Executive Compensation" {
			t.Fatal("SECURITY FAILURE: restricted document cited as a source for an unauthorized user")
		}
	}
}

func TestAskGrantsAccessToAuthorizedUser(t *testing.T) {
	e := setup(t)
	res, err := e.Ask(context.Background(), acl.User{Name: "ceo", Role: acl.RoleExec, Department: "exec"}, "executive compensation equity grants")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Answer.Text, "vesting over four years") {
		t.Fatalf("expected exec to receive restricted content they're authorized for, got %q", res.Answer.Text)
	}
	if res.SuppressedCount != 0 {
		t.Fatalf("expected no suppression for an authorized user, got %d", res.SuppressedCount)
	}
}

func TestEveryAskIsAudited(t *testing.T) {
	dir := t.TempDir()
	ix, _ := search.Open(filepath.Join(dir, "search.db"))
	defer ix.Close()
	al, _ := audit.Open(filepath.Join(dir, "audit.db"))
	defer al.Close()

	for _, c := range docstore.ChunkDocument(docstore.Document{ID: "d", Title: "t", Body: "content here"}) {
		ix.Add(c)
	}

	e := New(ix, synth.Fake{}, al)
	e.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	e.Ask(context.Background(), acl.User{Name: "alice", Role: acl.RoleEmployee}, "content here")
	e.Ask(context.Background(), acl.User{Name: "bob", Role: acl.RoleManager}, "something else")

	entries, err := al.Review(acl.User{Role: acl.RoleHRAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(entries))
	}
	if entries[0].UserName != "alice" || entries[1].UserName != "bob" {
		t.Fatalf("unexpected audit entries: %+v", entries)
	}
}

func TestAuditEntryReflectsActualSuppressedCount(t *testing.T) {
	e := setup(t)
	e.Ask(context.Background(), acl.User{Name: "eve", Role: acl.RoleEmployee, Department: "sales"}, "executive compensation equity grants")

	entries, err := e.Audit.Review(acl.User{Role: acl.RoleHRAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0].SuppressedCount != 1 {
		t.Fatalf("expected the audit entry to record the suppression, got %+v", entries[0])
	}
}
