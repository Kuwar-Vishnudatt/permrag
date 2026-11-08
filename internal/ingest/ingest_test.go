package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
)

func writeManifestDir(t *testing.T, manifestYAML string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadManifestBasic(t *testing.T) {
	dir := writeManifestDir(t, `
documents:
  - id: holidays
    title: Holiday Calendar
    file: holidays.md
  - id: comp
    title: Compensation Bands
    file: comp.md
    min_role: hr_admin
    departments: [hr]
`, map[string]string{
		"holidays.md": "Office closed on national holidays.",
		"comp.md":     "Confidential band details.",
	})

	docs, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}

	byID := map[string]int{}
	for i, d := range docs {
		byID[d.ID] = i
	}
	comp := docs[byID["comp"]]
	if comp.Policy.MinRole != acl.RoleHRAdmin {
		t.Fatalf("expected comp doc to require hr_admin, got %+v", comp.Policy)
	}
	if len(comp.Policy.Departments) != 1 || comp.Policy.Departments[0] != "hr" {
		t.Fatalf("expected comp doc to be restricted to hr department, got %+v", comp.Policy)
	}

	holidays := docs[byID["holidays"]]
	if holidays.Policy.MinRole != "" || len(holidays.Policy.Departments) != 0 {
		t.Fatalf("expected holidays doc to be unrestricted, got %+v", holidays.Policy)
	}
}

func TestLoadManifestRejectsDuplicateIDs(t *testing.T) {
	dir := writeManifestDir(t, `
documents:
  - id: a
    title: A
    file: a.md
  - id: a
    title: A again
    file: a.md
`, map[string]string{"a.md": "content"})

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error for duplicate document ids")
	}
}

func TestLoadManifestRejectsUnknownRole(t *testing.T) {
	dir := writeManifestDir(t, `
documents:
  - id: a
    title: A
    file: a.md
    min_role: superuser
`, map[string]string{"a.md": "content"})

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error for an unknown min_role")
	}
}

func TestLoadManifestErrorsOnMissingBodyFile(t *testing.T) {
	dir := writeManifestDir(t, `
documents:
  - id: a
    title: A
    file: does_not_exist.md
`, nil)

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error for a missing body file")
	}
}
