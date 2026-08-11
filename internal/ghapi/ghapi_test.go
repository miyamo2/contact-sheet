package ghapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --allow-fork is the gate; this is the guard rail behind it, so a token that
// cannot push has to come back false rather than as an error, and a response
// with no permissions object has to come back false rather than as a zero value
// read as a yes.
func TestWritable(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want bool
	}{
		{"a fork's read-only token", `{"permissions":{"pull":true,"push":false}}`, false},
		{"the base repository's token", `{"permissions":{"pull":true,"push":true,"admin":false}}`, true},
		{"no permissions reported", `{"full_name":"o/r"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/o/r" {
					t.Errorf("asked for %s", r.URL.Path)
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			got, err := New(server.URL, "token", "o/r").Writable(context.Background())
			if err != nil {
				t.Fatalf("Writable: %v", err)
			}
			if got != tt.want {
				t.Errorf("Writable = %v, want %v", got, tt.want)
			}
		})
	}
}

// A pull request from a fork is one this action can still comment on; what
// decides it is --allow-fork, not this.
func TestFromFork(t *testing.T) {
	var pull PullRequest
	pull.Base.Repo.FullName = "o/r"

	for _, tt := range []struct {
		head string
		want bool
	}{
		{"o/r", false},
		{"someone/r", true},
		// a fork deleted since the pull request was opened reports no head
		// repository at all, and guessing "fork" from that would take a pull
		// request out of reach for a token that can in fact write
		{"", false},
	} {
		pull.Head.Repo.FullName = tt.head
		if got := pull.FromFork(); got != tt.want {
			t.Errorf("head %q: FromFork = %v, want %v", tt.head, got, tt.want)
		}
	}
}
