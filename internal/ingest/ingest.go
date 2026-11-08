// Package ingest loads documents and their access policies from a
// directory: a manifest.yaml listing each document's metadata plus a
// relative path to its body text. Keeping the policy declaration in the
// manifest (not inferred from, say, a folder name or filename convention)
// is deliberate — an access decision should be an explicit, readable,
// reviewable statement, not something a typo in a directory name could
// silently get wrong.
package ingest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
	"github.com/Kuwar-Vishnudatt/permrag/internal/docstore"
)

type manifestEntry struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	File        string   `yaml:"file"`
	MinRole     string   `yaml:"min_role"`
	Departments []string `yaml:"departments"`
}

type manifest struct {
	Documents []manifestEntry `yaml:"documents"`
}

// LoadManifest reads <dir>/manifest.yaml and the body file for each listed
// document, returning fully-populated docstore.Document values.
func LoadManifest(dir string) ([]docstore.Document, error) {
	manifestPath := filepath.Join(dir, "manifest.yaml")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	seen := map[string]bool{}
	docs := make([]docstore.Document, 0, len(m.Documents))
	for _, e := range m.Documents {
		if e.ID == "" {
			return nil, fmt.Errorf("manifest entry missing id (title %q)", e.Title)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("duplicate document id %q in manifest", e.ID)
		}
		seen[e.ID] = true

		if e.MinRole != "" {
			switch acl.Role(e.MinRole) {
			case acl.RoleEmployee, acl.RoleManager, acl.RoleHRAdmin, acl.RoleExec:
			default:
				return nil, fmt.Errorf("document %q: unknown min_role %q", e.ID, e.MinRole)
			}
		}

		bodyPath := filepath.Join(dir, e.File)
		body, err := os.ReadFile(bodyPath)
		if err != nil {
			return nil, fmt.Errorf("document %q: read body file %q: %w", e.ID, e.File, err)
		}

		docs = append(docs, docstore.Document{
			ID:    e.ID,
			Title: e.Title,
			Body:  string(body),
			Policy: acl.Policy{
				MinRole:     acl.Role(e.MinRole),
				Departments: e.Departments,
			},
		})
	}
	return docs, nil
}
