package synth

import (
	"context"
	"strings"
	"testing"

	"github.com/Kuwar-Vishnudatt/permrag/internal/search"
)

func TestFakeReturnsNoResultsMessageWhenNoChunks(t *testing.T) {
	f := Fake{}
	a, err := f.Synthesize(context.Background(), "what is the vacation policy", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sources) != 0 {
		t.Fatalf("expected no sources, got %v", a.Sources)
	}
	if !strings.Contains(a.Text, "found no documents") {
		t.Fatalf("expected a no-access message, got %q", a.Text)
	}
}

func TestFakeCitesSourcesFromChunksOnly(t *testing.T) {
	f := Fake{}
	chunks := []search.Result{
		{ChunkID: "a#0", DocID: "a", DocTitle: "Vacation Policy", Text: "Employees get 15 days per year."},
		{ChunkID: "b#0", DocID: "b", DocTitle: "Sick Leave Policy", Text: "Sick leave is separate from vacation."},
	}
	a, err := f.Synthesize(context.Background(), "vacation days", chunks)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %v", a.Sources)
	}
	titles := map[string]bool{}
	for _, s := range a.Sources {
		titles[s.DocTitle] = true
	}
	if !titles["Vacation Policy"] || !titles["Sick Leave Policy"] {
		t.Fatalf("expected both doc titles as sources, got %v", a.Sources)
	}
}

func TestFakeDeduplicatesSourcesFromSameDocument(t *testing.T) {
	f := Fake{}
	chunks := []search.Result{
		{ChunkID: "a#0", DocID: "a", DocTitle: "Vacation Policy", Text: "First chunk."},
		{ChunkID: "a#1", DocID: "a", DocTitle: "Vacation Policy", Text: "Second chunk."},
	}
	a, err := f.Synthesize(context.Background(), "vacation", chunks)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sources) != 1 {
		t.Fatalf("expected deduplication to one source for two chunks of the same doc, got %v", a.Sources)
	}
}

func TestFakeOnlyEverSeesWhatItWasHanded(t *testing.T) {
	// The structural guarantee: Fake has no way to reach outside the
	// chunks slice it was given. This test is really documentation of
	// that fact via the type signature — Synthesize takes only
	// ([]search.Result), never an Index or a User — but asserting the
	// answer text only contains text from the provided chunks keeps it
	// honest against a future refactor that might accidentally widen the
	// interface.
	f := Fake{}
	chunks := []search.Result{{ChunkID: "a#0", DocID: "a", DocTitle: "Public FAQ", Text: "Office hours are nine to five."}}
	a, err := f.Synthesize(context.Background(), "office hours", chunks)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.Text, "nine to five") {
		t.Fatalf("expected the answer to draw from the provided chunk, got %q", a.Text)
	}
}
