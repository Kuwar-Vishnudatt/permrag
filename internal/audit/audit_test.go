package audit

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Kuwar-Vishnudatt/permrag/internal/acl"
)

func openTestLog(t *testing.T) *Log {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.db")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestRecordAndReviewByHRAdmin(t *testing.T) {
	l := openTestLog(t)
	err := l.Record(Entry{
		Time: time.Now(), UserName: "alice", UserRole: acl.RoleEmployee, UserDepartment: "sales",
		Query: "vacation policy", PermittedCount: 2, SuppressedCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := l.Review(acl.User{Name: "hr-bob", Role: acl.RoleHRAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].UserName != "alice" || entries[0].Query != "vacation policy" {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}
}

func TestReviewDeniedToEmployee(t *testing.T) {
	l := openTestLog(t)
	l.Record(Entry{Time: time.Now(), UserName: "alice", UserRole: acl.RoleEmployee, Query: "q"})

	entries, err := l.Review(acl.User{Name: "eve", Role: acl.RoleEmployee})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("SECURITY FAILURE: an employee must not be able to review the audit log, got %d entries", len(entries))
	}
}

func TestReviewDeniedToManager(t *testing.T) {
	// Manager is a higher rank than employee but still not HR/exec — the
	// audit log review gate is role-SPECIFIC (cross-departmental roles
	// only), not just "high enough rank."
	l := openTestLog(t)
	l.Record(Entry{Time: time.Now(), UserName: "alice", UserRole: acl.RoleEmployee, Query: "q"})

	entries, err := l.Review(acl.User{Name: "mgr", Role: acl.RoleManager})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected manager to be denied audit log review, got %d entries", len(entries))
	}
}

func TestExecCanReviewAuditLog(t *testing.T) {
	l := openTestLog(t)
	l.Record(Entry{Time: time.Now(), UserName: "alice", UserRole: acl.RoleEmployee, Query: "q"})

	entries, err := l.Review(acl.User{Name: "ceo", Role: acl.RoleExec})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exec to review the audit log, got %d entries", len(entries))
	}
}

func TestMultipleEntriesReturnedInOrder(t *testing.T) {
	l := openTestLog(t)
	l.Record(Entry{Time: time.Now(), UserName: "a", UserRole: acl.RoleEmployee, Query: "first"})
	l.Record(Entry{Time: time.Now(), UserName: "b", UserRole: acl.RoleEmployee, Query: "second"})

	entries, err := l.Review(acl.User{Role: acl.RoleHRAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Query != "first" || entries[1].Query != "second" {
		t.Fatalf("expected entries in insertion order, got %+v", entries)
	}
}
