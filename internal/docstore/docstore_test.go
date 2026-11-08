package docstore

import (
	"strings"
	"testing"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
)

func TestChunkInheritsDocumentPolicy(t *testing.T) {
	doc := Document{
		ID: "d1", Title: "Comp Bands", Body: "paragraph one.\n\nparagraph two.",
		Policy: acl.Policy{MinRole: acl.RoleHRAdmin},
	}
	chunks := ChunkDocument(doc)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.Policy.MinRole != acl.RoleHRAdmin {
			t.Fatalf("chunk %s did not inherit the document's policy, got %+v", c.ID, c.Policy)
		}
		if c.DocID != "d1" || c.DocTitle != "Comp Bands" {
			t.Fatalf("chunk %s missing correct doc linkage: %+v", c.ID, c)
		}
	}
}

func TestParagraphsSplitOnBlankLines(t *testing.T) {
	doc := Document{ID: "d1", Title: "t", Body: "first para.\n\nsecond para.\n\nthird para."}
	chunks := ChunkDocument(doc)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (one per paragraph), got %d", len(chunks))
	}
}

func TestEmptyParagraphsAreSkipped(t *testing.T) {
	doc := Document{ID: "d1", Title: "t", Body: "first.\n\n\n\nsecond."}
	chunks := ChunkDocument(doc)
	if len(chunks) != 2 {
		t.Fatalf("expected empty paragraphs to be skipped, got %d chunks", len(chunks))
	}
}

func TestLongParagraphIsSplitWithOverlap(t *testing.T) {
	words := make([]string, 300)
	for i := range words {
		words[i] = "word"
	}
	doc := Document{ID: "d1", Title: "t", Body: strings.Join(words, " ")}
	chunks := ChunkDocument(doc)
	if len(chunks) < 2 {
		t.Fatalf("expected a 300-word paragraph to split into multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		wordCount := len(strings.Fields(c.Text))
		if wordCount > maxChunkWords {
			t.Fatalf("chunk %s exceeds max words: %d > %d", c.ID, wordCount, maxChunkWords)
		}
	}
}

func TestChunkIDsAreUniqueAndStable(t *testing.T) {
	doc := Document{ID: "d1", Title: "t", Body: "a.\n\nb.\n\nc."}
	chunks := ChunkDocument(doc)
	seen := map[string]bool{}
	for _, c := range chunks {
		if seen[c.ID] {
			t.Fatalf("duplicate chunk ID %s", c.ID)
		}
		seen[c.ID] = true
	}
}
