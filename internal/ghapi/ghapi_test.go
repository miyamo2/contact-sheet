package ghapi

import "testing"

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
