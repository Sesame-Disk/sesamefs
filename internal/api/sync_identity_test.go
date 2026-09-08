package api

import "testing"

func TestSyncCommitStoredIdentityMatchesSnapshot(t *testing.T) {
	parent := "parent-1"
	cases := []struct {
		name     string
		existing map[string]interface{}
		want     *string
		root     string
		matches  bool
	}{
		{name: "same parent and root", existing: map[string]interface{}{"parent_id": parent, "root_fs_id": "root-1"}, want: &parent, root: "root-1", matches: true},
		{name: "nil parent matches empty stored parent", existing: map[string]interface{}{"parent_id": nil, "root_fs_id": "root-1"}, root: "root-1", matches: true},
		{name: "byte values match", existing: map[string]interface{}{"parent_id": []byte(parent), "root_fs_id": []byte("root-1")}, want: &parent, root: "root-1", matches: true},
		{name: "different root conflicts", existing: map[string]interface{}{"parent_id": parent, "root_fs_id": "root-2"}, want: &parent, root: "root-1", matches: false},
		{name: "different parent conflicts", existing: map[string]interface{}{"parent_id": "parent-2", "root_fs_id": "root-1"}, want: &parent, root: "root-1", matches: false},
		{name: "missing identity conflicts", existing: map[string]interface{}{"root_fs_id": "root-1"}, want: &parent, root: "root-1", matches: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := syncCommitStoredIdentityMatches(tc.existing, tc.want, tc.root); got != tc.matches {
				t.Fatalf("syncCommitStoredIdentityMatches = %v, want %v", got, tc.matches)
			}
		})
	}
}
