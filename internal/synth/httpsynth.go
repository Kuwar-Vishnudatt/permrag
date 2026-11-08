package synth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kuwar-Vishnudatt/permrag/internal/search"
)

// HTTPSynth talks to any OpenAI-compatible /chat/completions endpoint. It
// is handed the exact same already-filtered []search.Result as Fake — the
// prompt is built entirely from that slice, so a real LLM call carries no
// more access to restricted content than the fake one does.
type HTTPSynth struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func NewHTTPSynth(baseURL, apiKey, model string) *HTTPSynth {
	return &HTTPSynth{BaseURL: baseURL, APIKey: apiKey, Model: model, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (h *HTTPSynth) Synthesize(ctx context.Context, query string, chunks []search.Result) (Answer, error) {
	if len(chunks) == 0 {
		return Answer{Text: fmt.Sprintf("I found no documents you have access to that answer %q.", query)}, nil
	}

	var context_ strings.Builder
	seenTitles := map[string]bool{}
	var sources []Source
	for i, c := range chunks {
		fmt.Fprintf(&context_, "[%d] (%s) %s\n\n", i+1, c.DocTitle, c.Text)
		if !seenTitles[c.DocTitle] {
			seenTitles[c.DocTitle] = true
			sources = append(sources, Source{DocTitle: c.DocTitle, DocID: c.DocID})
		}
	}

	prompt := fmt.Sprintf(
		"Answer the question using ONLY the excerpts below. If they don't contain the answer, say so.\n\nExcerpts:\n%s\nQuestion: %s",
		context_.String(), query,
	)

	reqBody, _ := json.Marshal(map[string]any{
		"model": h.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, err := http.NewRequestWithContext(ctx, "POST", h.BaseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return Answer{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.APIKey)
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		return Answer{}, fmt.Errorf("synth request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Answer{}, fmt.Errorf("synth request failed: %s: %s", resp.Status, body)
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Answer{}, fmt.Errorf("parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Answer{}, fmt.Errorf("no choices in response")
	}

	return Answer{Text: parsed.Choices[0].Message.Content, Sources: sources}, nil
}
