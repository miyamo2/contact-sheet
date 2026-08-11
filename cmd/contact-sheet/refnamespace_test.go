package main

import (
	"strings"
	"testing"
)

func TestCheckRefNamespaceAccepts(t *testing.T) {
	tests := map[string]string{
		"the default":            "refs/contact-sheet",
		"a deeper hierarchy":     "refs/contact-sheet/e2e",
		"a trailing slash":       "refs/contact-sheet/",
		"space around the value": "  refs/contact-sheet  ",
		// heads and tags are the reserved ones; a hierarchy that merely reads
		// like one of them is somebody's own
		"a name starting with a reserved one": "refs/headstones",
		"a dot inside a component":            "refs/contact.sheet",
		"a name a forge does not claim":       "refs/screenshots/pr",
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := checkRefNamespace(value)
			if err != nil {
				t.Fatalf("checkRefNamespace(%q): %v", value, err)
			}
			if strings.HasSuffix(got, "/") || strings.TrimSpace(got) != got {
				t.Errorf("got %q, which is not the form the ref is composed from", got)
			}
		})
	}
}

// The two hierarchies with a reason behind them. A branch is the mistake the
// whole design exists to prevent, so the message has to say why rather than
// only that.
func TestCheckRefNamespaceRejectsBranchesAndTags(t *testing.T) {
	tests := map[string]struct{ value, mentions string }{
		"a branch namespace":   {"refs/heads/contact-sheet", "clone"},
		"refs/heads itself":    {"refs/heads", "clone"},
		"a trailing slash":     {"refs/heads/contact-sheet/", "clone"},
		"a tag namespace":      {"refs/tags/contact-sheet", "tag"},
		"refs/tags itself":     {"refs/tags", "tag"},
		"a deep tag namespace": {"refs/tags/ci/contact-sheet", "tag"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := checkRefNamespace(test.value)
			if err == nil {
				t.Fatalf("checkRefNamespace(%q): want an error", test.value)
			}
			if !strings.Contains(err.Error(), "--ref-namespace") {
				t.Errorf("the message does not name the input: %v", err)
			}
			if !strings.Contains(err.Error(), test.mentions) {
				t.Errorf("the message does not say why: %v", err)
			}
		})
	}
}

func TestCheckRefNamespaceRejects(t *testing.T) {
	tests := map[string]string{
		"the empty string":                "",
		"only spaces":                     "   ",
		"the root of every ref":           "refs",
		"a bare name":                     "contact-sheet",
		"a relative-looking one":          "../refs/contact-sheet",
		"a leading slash":                 "/refs/contact-sheet",
		"a doubled slash":                 "refs//contact-sheet",
		"two dots":                        "refs/contact..sheet",
		"a component ending .lock":        "refs/contact-sheet.lock",
		"a component starting with a dot": "refs/.contact-sheet",
		"a trailing dot":                  "refs/contact-sheet.",
		"a space inside":                  "refs/contact sheet",
		"a control character":             "refs/contact\x01sheet",
		"a tilde":                         "refs/contact~sheet",
		"a caret":                         "refs/contact^sheet",
		"a colon":                         "refs/contact:sheet",
		"a question mark":                 "refs/contact?sheet",
		"an asterisk":                     "refs/contact*sheet",
		"an open bracket":                 "refs/contact[sheet",
		"a backslash":                     `refs/contact\sheet`,
		"a reflog shorthand":              "refs/contact-sheet@{1}",
		// git's own hierarchies
		"remote-tracking branches": "refs/remotes/origin",
		"notes":                    "refs/notes/contact-sheet",
		"replacements":             "refs/replace",
		"the stash":                "refs/stash",
		// and the forges'
		"GitHub's pull refs":      "refs/pull/42/head",
		"GitLab's merge refs":     "refs/merge-requests/contact-sheet",
		"GitLab's other spelling": "refs/merge_requests/contact-sheet",
		"Bitbucket's":             "refs/pull-requests/contact-sheet",
		"Gerrit's changes":        "refs/changes/contact-sheet",
		"Gerrit's review push":    "refs/for/main",
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := checkRefNamespace(value)
			if err == nil {
				t.Fatalf("checkRefNamespace(%q) returned %q; want an error", value, got)
			}
			// whoever reads this is looking at a workflow file, and the input's
			// name is what they can search for in it
			if !strings.Contains(err.Error(), "--ref-namespace") {
				t.Errorf("the message does not name the input: %v", err)
			}
		})
	}
}

// The suffix this composes onto a validated namespace has to be a name git
// accepts, since nothing checks the composed ref again.
func TestComposedRefIsWellFormed(t *testing.T) {
	namespace, err := checkRefNamespace("refs/contact-sheet")
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{
		namespace + "/pr-42/12345678.1",
		namespace + "/pr-1/local.1",
	} {
		if err := checkRefFormat(ref); err != nil {
			t.Errorf("checkRefFormat(%q): %v", ref, err)
		}
	}
}
