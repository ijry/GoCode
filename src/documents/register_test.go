package documents

import (
	"testing"

	"github.com/egoist/mygo"
)

// TestRegisterIsValid guards the mygo.NewDocuments preconditions: it panics if
// the document type has no extension, Read/Write/Window is nil, or an extension
// is malformed. Register must satisfy them so the app boots.
func TestRegisterIsValid(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Register panicked: %v", r)
		}
	}()
	Register()
}

// TestDocStatusLabel locks the state→label mapping the document window shows,
// including the Conflict-over-Edited precedence.
func TestDocStatusLabel(t *testing.T) {
	cases := []struct {
		name  string
		state mygo.DocumentState
		want  string
	}{
		{"untitled", mygo.DocumentState{}, "Not saved"},
		{"read-only", mygo.DocumentState{Path: "/x.md", ReadOnly: true, Edited: true}, "Read-only"},
		{"conflict", mygo.DocumentState{Path: "/x.md", Edited: true, Conflict: true}, "Conflict"},
		{"edited", mygo.DocumentState{Path: "/x.md", Edited: true}, "Edited"},
		{"saved", mygo.DocumentState{Path: "/x.md"}, "Saved"},
	}
	for _, tc := range cases {
		if got := docStatusLabel(tc.state); got != tc.want {
			t.Errorf("%s: docStatusLabel = %q, want %q", tc.name, got, tc.want)
		}
	}
}
