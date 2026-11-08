// Package synth turns a query plus a set of ALREADY ACL-FILTERED chunks
// into an answer. Synthesizer never sees anything beyond what it's handed
// — it has no access to the document store or search index, so there is
// no path by which it could retrieve restricted content on its own even if
// instructed to by a cleverly-worded query. The security boundary is
// entirely upstream, in package search; this package is deliberately
// "dumb" about access control because it should never need to know about
// it at all.
package synth

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Kuwar-Vishnudatt/permrag/internal/search"
)

// Source is one chunk a synthesized answer cited.
type Source struct {
	DocTitle string
	DocID    string
}

// Answer is a synthesized response plus exactly which sources it drew on.
type Answer struct {
	Text    string
	Sources []Source
}

type Synthesizer interface {
	Synthesize(ctx context.Context, query string, chunks []search.Result) (Answer, error)
}

// Fake is a deterministic synthesizer: it doesn't call any LLM, just
// stitches together the permitted chunk texts with a templated preamble
// and a deduplicated source list. Used by every test and the demo, so
// behavior is fully reproducible with no API costs — the same pattern
// used for every Fake backend elsewhere in this portfolio (llmgate,
// evalbench).
type Fake struct{}

func (Fake) Synthesize(_ context.Context, query string, chunks []search.Result) (Answer, error) {
	if len(chunks) == 0 {
		return Answer{Text: fmt.Sprintf("I found no documents you have access to that answer %q.", query)}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Based on the documents you have access to:\n")
	seenTitles := map[string]bool{}
	var sources []Source
	for _, c := range chunks {
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(firstSentence(c.Text)))
		if !seenTitles[c.DocTitle] {
			seenTitles[c.DocTitle] = true
			sources = append(sources, Source{DocTitle: c.DocTitle, DocID: c.DocID})
		}
	}

	sort.Slice(sources, func(i, j int) bool { return sources[i].DocTitle < sources[j].DocTitle })
	return Answer{Text: b.String(), Sources: sources}, nil
}

func firstSentence(text string) string {
	if idx := strings.IndexAny(text, ".!?"); idx >= 0 && idx < 200 {
		return text[:idx+1]
	}
	if len(text) > 200 {
		return text[:200] + "..."
	}
	return text
}
