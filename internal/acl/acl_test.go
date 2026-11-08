package acl

import "testing"

func TestPublicPolicyAllowsEveryone(t *testing.T) {
	p := Policy{}
	u := User{Role: RoleEmployee, Department: "sales"}
	if !Allows(p, u) {
		t.Fatal("a zero-value (no restrictions) policy must allow everyone")
	}
}

func TestMinRoleGatesLowerRoles(t *testing.T) {
	p := Policy{MinRole: RoleManager}
	if Allows(p, User{Role: RoleEmployee}) {
		t.Fatal("employee must not satisfy a manager-and-above policy")
	}
	if !Allows(p, User{Role: RoleManager}) {
		t.Fatal("manager must satisfy a manager-and-above policy")
	}
	if !Allows(p, User{Role: RoleExec}) {
		t.Fatal("exec (higher rank) must satisfy a manager-and-above policy")
	}
}

func TestDepartmentRestrictionGatesOtherDepartments(t *testing.T) {
	p := Policy{Departments: []string{"engineering"}}
	if !Allows(p, User{Role: RoleEmployee, Department: "engineering"}) {
		t.Fatal("matching department must be allowed")
	}
	if Allows(p, User{Role: RoleEmployee, Department: "sales"}) {
		t.Fatal("non-matching department must be denied")
	}
}

func TestDepartmentRestrictionAllowsAnyListedDepartment(t *testing.T) {
	p := Policy{Departments: []string{"engineering", "sales"}}
	if !Allows(p, User{Role: RoleEmployee, Department: "sales"}) {
		t.Fatal("a department anywhere in the list must be allowed")
	}
	if Allows(p, User{Role: RoleEmployee, Department: "finance"}) {
		t.Fatal("a department not in the list must be denied")
	}
}

func TestHRAdminBypassesDepartmentRestriction(t *testing.T) {
	p := Policy{Departments: []string{"engineering"}}
	if !Allows(p, User{Role: RoleHRAdmin, Department: "sales"}) {
		t.Fatal("HR admin must see across departments regardless of their own department")
	}
}

func TestExecBypassesDepartmentRestriction(t *testing.T) {
	p := Policy{Departments: []string{"engineering"}}
	if !Allows(p, User{Role: RoleExec, Department: "finance"}) {
		t.Fatal("exec must see across departments regardless of their own department")
	}
}

func TestHRAdminStillSubjectToMinRole(t *testing.T) {
	// Cross-departmental access is not the same as unlimited access: an
	// hr_admin-ranked policy requirement still has to be satisfied.
	p := Policy{MinRole: RoleExec}
	if Allows(p, User{Role: RoleHRAdmin, Department: "any"}) {
		t.Fatal("hr_admin must not satisfy an exec-only MinRole just by being cross-departmental")
	}
}

func TestCombinedRoleAndDepartmentBothMustPass(t *testing.T) {
	p := Policy{MinRole: RoleManager, Departments: []string{"engineering"}}
	if Allows(p, User{Role: RoleEmployee, Department: "engineering"}) {
		t.Fatal("right department but wrong role rank must still be denied")
	}
	if Allows(p, User{Role: RoleManager, Department: "sales"}) {
		t.Fatal("right role rank but wrong department must still be denied")
	}
	if !Allows(p, User{Role: RoleManager, Department: "engineering"}) {
		t.Fatal("right role rank AND right department must be allowed")
	}
}

func TestUnknownRoleIsNeverGrantedAccessByMinRole(t *testing.T) {
	p := Policy{MinRole: RoleEmployee} // the lowest real rank
	if Allows(p, User{Role: Role("totally_made_up")}) {
		t.Fatal("an unrecognized role must fail-closed, never fail-open")
	}
}
