package main

import (
	"strings"
	"testing"
)

func TestBuildInfoShort(t *testing.T) {
	const revision = "796a036db3eb55a47fbee0377ba3d5dd5ef473f1"

	cases := []struct {
		name string
		info buildInfo
		want string
	}{{
		// a release: the tag names no commit, so the commit is worth adding
		name: "tag",
		info: buildInfo{Version: "v1.2.3", Revision: revision},
		want: "v1.2.3 (796a036)",
	}, {
		// `go install ...@<ref>`: the pseudo-version ends in the commit already
		name: "pseudo-version",
		info: buildInfo{Version: "v0.0.0-20260811044944-796a036db3eb", Revision: revision},
		want: "v0.0.0-20260811044944-796a036db3eb",
	}, {
		// the toolchain has marked it; saying so twice helps nobody
		name: "pseudo-version of a modified tree",
		info: buildInfo{Version: "v0.0.0-20260811044944-796a036db3eb+dirty", Revision: revision, Modified: true},
		want: "v0.0.0-20260811044944-796a036db3eb+dirty",
	}, {
		name: "tag of a modified tree",
		info: buildInfo{Version: "v1.2.3", Revision: revision, Modified: true},
		want: "v1.2.3 (796a036+dirty)",
	}, {
		// a build of a tree with no version control around it: all there is
		name: "no revision",
		info: buildInfo{Version: devel},
		want: devel,
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.info.Short(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// The binary is downloaded prebuilt by the action rather than built by whoever
// reads the output, so --version is the only place the platform it landed on is
// named.
func TestBuildInfoLong(t *testing.T) {
	got := buildInfo{Version: "v1.2.3", Revision: "796a036", Time: "2026-08-11T04:49:44Z"}.Long()
	for _, want := range []string{"contact-sheet v1.2.3", "796a036", "2026-08-11T04:49:44Z", "go:", "platform:"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// The binary under test is built from this repository, so it has real build
// information behind it: the fallback should not be what comes out.
func TestReadBuildInfo(t *testing.T) {
	if got := readBuildInfo(); got.Version == "" {
		t.Error("the version is empty; it should fall back to " + devel)
	}
}
