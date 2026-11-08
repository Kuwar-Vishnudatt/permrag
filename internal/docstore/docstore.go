// Package docstore defines documents and the chunking that splits them for
// indexing — the step where getting ACL propagation wrong would be most
// dangerous: a chunk is a standalone retrievable unit, so it must carry its
// parent document's access policy forward explicitly rather than relying
// on anything joined back to the parent at query time.
package docstore

import (
	"fmt"
	"strings"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
)

// Document is one source document before chunking.
type Document struct {
	ID     string
	Title  string
	Body   string
	Policy acl.Policy
}

// Chunk is one retrievable unit. Policy is a COPY of the parent document's
// policy, captured at chunking time — the entire point being that nothing
// downstream (the search index, the synthesizer) needs to trust a join
// back to the document table to know a chunk's access requirements.
type Chunk struct {
	ID       string
	DocID    string
	DocTitle string
	Text     string
	Policy   acl.Policy
}

// maxChunkWords caps how large one chunk can get before it's split
// further; overlapWords repeats a few trailing words of the previous
// chunk at the start of the next one, so a sentence that happens to fall
// exactly on a split boundary isn't rendered contextless in either half.
const (
	maxChunkWords = 120
	overlapWords  = 15
)

// Chunk splits a document into paragraph-bounded chunks, further splitting
// any paragraph that exceeds maxChunkWords. Every chunk gets a copy of
// doc.Policy.
func ChunkDocument(doc Document) []Chunk {
	paragraphs := splitParagraphs(doc.Body)

	var chunks []Chunk
	idx := 0
	for _, para := range paragraphs {
		for _, text := range splitLong(para, maxChunkWords, overlapWords) {
			chunks = append(chunks, Chunk{
				ID:       fmt.Sprintf("%s#%d", doc.ID, idx),
				DocID:    doc.ID,
				DocTitle: doc.Title,
				Text:     text,
				Policy:   doc.Policy,
			})
			idx++
		}
	}
	return chunks
}

func splitParagraphs(body string) []string {
	raw := strings.Split(body, "\n\n")
	var out []string
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func splitLong(para string, maxWords, overlap int) []string {
	words := strings.Fields(para)
	if len(words) <= maxWords {
		return []string{para}
	}

	var out []string
	start := 0
	for start < len(words) {
		end := start + maxWords
		if end > len(words) {
			end = len(words)
		}
		out = append(out, strings.Join(words[start:end], " "))
		if end == len(words) {
			break
		}
		start = end - overlap
	}
	return out
}
