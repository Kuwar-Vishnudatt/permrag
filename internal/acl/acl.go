// Package acl defines the access-control model this RAG system enforces:
// a role hierarchy (so a document can require "manager and above" rather
// than enumerating every allowed role) plus an optional department
// restriction, with HR and execs treated as cross-departmental roles — the
// realistic shape of "who gets to see this" in an actual HR/ops org.
package acl

// Role is a role in the org's access hierarchy, ordered low to high.
type Role string

const (
	RoleEmployee Role = "employee"
	RoleManager  Role = "manager"
	RoleHRAdmin  Role = "hr_admin"
	RoleExec     Role = "exec"
)

// rank gives each role a comparable rank so a document can declare a
// MINIMUM role ("manager and above") instead of enumerating every role
// that should see it — the realistic way HR/ops policies are actually
// written, and one that doesn't need updating every time a new role is
// added above the ones already allowed.
var rank = map[Role]int{
	RoleEmployee: 0,
	RoleManager:  1,
	RoleHRAdmin:  2,
	RoleExec:     3,
}

// Rank returns a role's numeric rank, or -1 for an unknown role (which
// then fails every MinRole comparison safely closed, rather than silently
// granting access to an unrecognized role).
func Rank(r Role) int {
	if v, ok := rank[r]; ok {
		return v
	}
	return -1
}

// IsCrossDepartmental reports whether a role's access isn't confined to
// its own department — true for HR and exec, who legitimately need to see
// across-department material (compensation reviews spanning every team,
// company-wide planning docs) that a same-ranked but department-bound role
// would not.
func IsCrossDepartmental(r Role) bool {
	return r == RoleHRAdmin || r == RoleExec
}

// User is the requester of a query.
type User struct {
	Name       string
	Role       Role
	Department string
}

// Policy is a document's (or chunk's) access requirement. The zero value
// (MinRole: "", Departments: nil) means "visible to everyone" — an
// explicit, not accidental, choice: a document with no declared policy is
// public, which is the right default for e.g. the company holiday
// calendar, not a fail-open bug for something that should have been
// restricted. Authoring a restricted document always means setting
// MinRole and/or Departments explicitly.
type Policy struct {
	MinRole     Role
	Departments []string // empty = all departments
}

// Allows reports whether u satisfies p.
func Allows(p Policy, u User) bool {
	if p.MinRole != "" && Rank(u.Role) < Rank(p.MinRole) {
		return false
	}
	if len(p.Departments) > 0 && !IsCrossDepartmental(u.Role) {
		if !contains(p.Departments, u.Department) {
			return false
		}
	}
	return true
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
