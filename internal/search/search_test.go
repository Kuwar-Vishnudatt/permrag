package search

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
	"github.com/Kuwar-Vishnudatt/permrag/internal/docstore"
)

func openTestIndex(t *testing.T) *Index {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

func addDoc(t *testing.T, ix *Index, id, title, body string, policy acl.Policy) {
	t.Helper()
	doc := docstore.Document{ID: id, Title: title, Body: body, Policy: policy}
	for _, c := range docstore.ChunkDocument(doc) {
		if err := ix.Add(c); err != nil {
			t.Fatal(err)
		}
	}
}

func chunkIDs(results []Result) map[string]bool {
	m := map[string]bool{}
	for _, r := range results {
		m[r.ChunkID] = true
	}
	return m
}

func TestPublicDocumentIsVisibleToEveryone(t *testing.T) {
	ix := openTestIndex(t)
	addDoc(t, ix, "d1", "Holiday Calendar", "The office is closed on national holidays.", acl.Policy{})

	res, err := ix.Search("holidays", acl.User{Role: acl.RoleEmployee, Department: "sales"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected the public doc to be found, got %+v", res)
	}
}

func TestRoleRestrictedDocumentIsHiddenFromLowerRole(t *testing.T) {
	ix := openTestIndex(t)
	addDoc(t, ix, "d1", "Executive Compensation Plan", "Exec salary bands are strictly confidential information.",
		acl.Policy{MinRole: acl.RoleExec})

	res, err := ix.Search("salary bands confidential", acl.User{Role: acl.RoleEmployee}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 0 {
		t.Fatalf("SECURITY FAILURE: employee must not see exec-only content, got %+v", res.Results)
	}
	if res.SuppressedCount != 1 {
		t.Fatalf("expected the suppressed count to reflect the withheld match, got %d", res.SuppressedCount)
	}
}

func TestRoleRestrictedDocumentIsVisibleToAllowedRole(t *testing.T) {
	ix := openTestIndex(t)
	addDoc(t, ix, "d1", "Exec Comp Plan", "Exec salary bands are confidential.", acl.Policy{MinRole: acl.RoleExec})

	res, err := ix.Search("salary bands", acl.User{Role: acl.RoleExec}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected exec to see exec-only content, got %+v", res.Results)
	}
	if res.SuppressedCount != 0 {
		t.Fatalf("expected no suppressed count for a user who is allowed to see it, got %d", res.SuppressedCount)
	}
}

func TestSuppressedCountNeverLeaksTitleOrContent(t *testing.T) {
	// This is the key non-leak assertion: even though we KNOW a restricted
	// match exists (SuppressedCount > 0), nothing about what it contains —
	// not the title, not any text — is anywhere in the SearchResult for an
	// unauthorized user.
	ix := openTestIndex(t)
	addDoc(t, ix, "secret1", "Q4 Layoffs Plan - CONFIDENTIAL", "layoffs reorganization headcount reduction plan",
		acl.Policy{MinRole: acl.RoleExec})

	res, err := ix.Search("layoffs reorganization", acl.User{Role: acl.RoleEmployee}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 0 {
		t.Fatalf("expected zero visible results, got %+v", res.Results)
	}
	if res.SuppressedCount != 1 {
		t.Fatalf("expected suppressed count 1, got %d", res.SuppressedCount)
	}
	// Structural guarantee: SearchResult's only numeric field about
	// suppressed matches is SuppressedCount — there is no title/body field
	// that could carry this information even by programmer mistake,
	// because Results itself is empty.
}

func TestDepartmentRestrictionEnforcedInSearch(t *testing.T) {
	ix := openTestIndex(t)
	addDoc(t, ix, "d1", "Engineering Roadmap", "Our roadmap covers the new platform migration.",
		acl.Policy{Departments: []string{"engineering"}})

	resSales, _ := ix.Search("roadmap platform migration", acl.User{Role: acl.RoleEmployee, Department: "sales"}, 10)
	if len(resSales.Results) != 0 {
		t.Fatalf("SECURITY FAILURE: sales employee must not see engineering-only content, got %+v", resSales.Results)
	}

	resEng, _ := ix.Search("roadmap platform migration", acl.User{Role: acl.RoleEmployee, Department: "engineering"}, 10)
	if len(resEng.Results) != 1 {
		t.Fatalf("expected engineering employee to see their own department's content, got %+v", resEng.Results)
	}
}

func TestHRAdminSeesAcrossDepartmentsInSearch(t *testing.T) {
	ix := openTestIndex(t)
	addDoc(t, ix, "d1", "Sales Commission Structure", "Commission payout structure details for sales staff.",
		acl.Policy{Departments: []string{"sales"}})

	res, err := ix.Search("commission payout structure", acl.User{Role: acl.RoleHRAdmin, Department: "engineering"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected HR admin to see sales-restricted content despite being in engineering, got %+v", res.Results)
	}
}

func TestCombinedRoleAndDepartmentBothEnforced(t *testing.T) {
	ix := openTestIndex(t)
	addDoc(t, ix, "d1", "Engineering Manager Reviews", "Performance review notes for the engineering team leads.",
		acl.Policy{MinRole: acl.RoleManager, Departments: []string{"engineering"}})

	cases := []struct {
		name string
		u    acl.User
		want int
	}{
		{"right dept wrong role", acl.User{Role: acl.RoleEmployee, Department: "engineering"}, 0},
		{"right role wrong dept", acl.User{Role: acl.RoleManager, Department: "sales"}, 0},
		{"right role right dept", acl.User{Role: acl.RoleManager, Department: "engineering"}, 1},
	}
	for _, c := range cases {
		res, err := ix.Search("performance review notes team leads", c.u, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Results) != c.want {
			t.Errorf("%s: expected %d results, got %d (%+v)", c.name, c.want, len(res.Results), res.Results)
		}
	}
}

func TestAdversarialExactKeywordQueryStillRespectsACL(t *testing.T) {
	// A deliberately crafted query using the restricted document's exact
	// wording must not bypass the filter just because it ranks extremely
	// well — the WHERE clause applies before ranking, not after.
	ix := openTestIndex(t)
	addDoc(t, ix, "secret1", "Exec Salary Bands 2026",
		"exec salary bands 2026 confidential compensation figures do not distribute",
		acl.Policy{MinRole: acl.RoleExec})

	res, err := ix.Search("exec salary bands 2026 confidential compensation figures do not distribute",
		acl.User{Role: acl.RoleManager}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 0 {
		t.Fatalf("SECURITY FAILURE: an exact-keyword adversarial query must not bypass ACL filtering, got %+v", res.Results)
	}
}

func TestMultipleChunksOfRestrictedDocAllRespectACL(t *testing.T) {
	// A long restricted document produces multiple chunks; every single
	// one must inherit the restriction, not just the first.
	ix := openTestIndex(t)
	body := strings.Repeat("confidential ", 300)
	addDoc(t, ix, "secret1", "Big Secret Doc", body, acl.Policy{MinRole: acl.RoleExec})

	res, err := ix.Search("confidential", acl.User{Role: acl.RoleEmployee}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 0 {
		t.Fatalf("SECURITY FAILURE: expected zero chunks visible across the whole restricted document, got %d", len(res.Results))
	}
}

func TestResultsOrderedBestMatchFirst(t *testing.T) {
	// FTS5's bm25() returns MORE NEGATIVE for a better match — this test
	// exists specifically to catch an inverted ORDER BY, which would
	// silently return the worst matches first while looking superficially
	// fine (same rows, wrong order).
	ix := openTestIndex(t)
	addDoc(t, ix, "strong", "Strong Match", "cat cat cat cat cat", acl.Policy{})
	addDoc(t, ix, "weak", "Weak Match", "cat dog bird fish tree house car plane boat train bicycle scooter skateboard",
		acl.Policy{})

	res, err := ix.Search("cat", acl.User{Role: acl.RoleEmployee}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res.Results))
	}
	if res.Results[0].ChunkID != "strong#0" {
		t.Fatalf("expected the stronger match first, got order: %v", chunkIDs(res.Results))
	}
}

func TestQueryWithPunctuationDoesNotError(t *testing.T) {
	ix := openTestIndex(t)
	addDoc(t, ix, "d1", "Section 3", "Section 3: Benefits details go here.", acl.Policy{})

	res, err := ix.Search(`Section 3: "Benefits"?!`, acl.User{Role: acl.RoleEmployee}, 10)
	if err != nil {
		t.Fatalf("expected punctuation in the query to be sanitized, not error: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected the sanitized query to still match, got %+v", res.Results)
	}
}

func TestEmptyQueryReturnsNoResults(t *testing.T) {
	ix := openTestIndex(t)
	addDoc(t, ix, "d1", "t", "some content here", acl.Policy{})
	res, err := ix.Search("???", acl.User{Role: acl.RoleEmployee}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 0 {
		t.Fatalf("expected a query with no usable terms to return nothing, got %+v", res.Results)
	}
}
