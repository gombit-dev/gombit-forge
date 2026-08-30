package org

import "testing"

func TestRoleValid(t *testing.T) {
	for _, r := range []Role{RoleOwner, RoleAdmin, RoleMember} {
		if !r.Valid() {
			t.Errorf("%q should be valid", r)
		}
	}
	for _, r := range []Role{"", "superuser", "Owner", "guest"} {
		if r.Valid() {
			t.Errorf("%q should be invalid", r)
		}
	}
}

// TestCanMatrix pins the role → capability matrix so a change to it is a
// deliberate, reviewed edit rather than an accident.
func TestCanMatrix(t *testing.T) {
	all := []Capability{
		CapOrgView, CapOrgManage, CapMembersView, CapMembersInvite,
		CapMembersRemove, CapProjectCreate, CapProjectDelete,
	}

	// want[role] is the exact set of capabilities that role must have; every
	// other capability must be denied.
	want := map[Role]map[Capability]bool{
		RoleOwner: {
			CapOrgView: true, CapOrgManage: true, CapMembersView: true,
			CapMembersInvite: true, CapMembersRemove: true,
			CapProjectCreate: true, CapProjectDelete: true,
		},
		RoleAdmin: {
			CapOrgView: true, CapMembersView: true, CapMembersInvite: true,
			CapMembersRemove: true, CapProjectCreate: true, CapProjectDelete: true,
		},
		RoleMember: {
			CapOrgView: true, CapMembersView: true, CapProjectCreate: true,
		},
	}

	for role, allowed := range want {
		for _, c := range all {
			got := Can(role, c)
			if got != allowed[c] {
				t.Errorf("Can(%q, %q) = %v, want %v", role, c, got, allowed[c])
			}
		}
	}

	// Owner must not be able to escape the org-manage line held from admin:
	// admin explicitly cannot manage/delete the org.
	if Can(RoleAdmin, CapOrgManage) {
		t.Error("admin must not have org.manage")
	}
	if Can(RoleMember, CapMembersInvite) {
		t.Error("member must not be able to invite")
	}
}

// TestCanFailsClosed confirms an unknown role permits nothing.
func TestCanFailsClosed(t *testing.T) {
	for _, c := range []Capability{CapOrgView, CapProjectCreate, CapMembersInvite} {
		if Can("bogus", c) {
			t.Errorf("unknown role must not permit %q", c)
		}
	}
}

// TestCanGrant pins the grant hierarchy: a role may only grant roles at or
// below its own standing, and unknown roles neither grant nor are grantable.
func TestCanGrant(t *testing.T) {
	cases := []struct {
		granter, target Role
		want            bool
	}{
		{RoleOwner, RoleOwner, true},
		{RoleOwner, RoleAdmin, true},
		{RoleOwner, RoleMember, true},
		{RoleAdmin, RoleOwner, false}, // the escalation this gate exists to stop
		{RoleAdmin, RoleAdmin, true},
		{RoleAdmin, RoleMember, true},
		{RoleMember, RoleOwner, false},
		{RoleMember, RoleAdmin, false},
		{RoleMember, RoleMember, true},
		{"bogus", RoleMember, false},
		{RoleOwner, "bogus", false},
	}
	for _, c := range cases {
		if got := CanGrant(c.granter, c.target); got != c.want {
			t.Errorf("CanGrant(%q, %q) = %v, want %v", c.granter, c.target, got, c.want)
		}
	}
}
