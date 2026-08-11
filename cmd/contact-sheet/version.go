package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// build is what this binary knows about itself.
//
// It is read once, at start, from the build information the toolchain stamps
// into every binary rather than from a variable set with -ldflags. Go embeds
// the main module's version and the commit it was built from, so a release
// binary names its own tag without a release having to write that tag into a
// file in the tree first -- which is the whole reason there is no VERSION file
// any more.
var build = readBuildInfo()

// buildInfo is the subset of debug.BuildInfo worth printing.
//
// Version is the main module's version. Built from a checkout standing on a
// tag it is that tag; installed with `go install ...@<commit>` it is the
// pseudo-version the module proxy assigns that commit; built from a tree with
// no version control around it -- which is what a `go build` in a source
// tarball is -- it is "(devel)", and Revision is empty with it. That last case
// is why Revision is printed at all: it is the only thing left that identifies
// such a build.
type buildInfo struct {
	Version  string
	Revision string
	Time     string
	Modified bool
}

// devel is what the toolchain reports for a build no version has been settled
// on, and what this falls back to when there is no build information at all.
const devel = "(devel)"

func readBuildInfo() buildInfo {
	info := buildInfo{Version: devel}
	raw, ok := debug.ReadBuildInfo()
	if !ok {
		// a binary built by something that stripped the build info, or a test
		// binary on an old toolchain. Nothing to say beyond the fallback
		return info
	}
	if raw.Main.Version != "" {
		info.Version = raw.Main.Version
	}
	for _, setting := range raw.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.Revision = setting.Value
		case "vcs.time":
			info.Time = setting.Value
		case "vcs.modified":
			info.Modified = setting.Value == "true"
		}
	}
	return info
}

// Short is the one-line form. The version leads and the commit follows it only
// where it adds something: a pseudo-version already ends in the commit it was
// built from, and the toolchain has already marked a modified tree there, but a
// tag -- or "(devel)" -- names no commit at all.
func (b buildInfo) Short() string {
	if b.Revision == "" || strings.Contains(b.Version, short(b.Revision)) {
		return b.Version
	}
	revision := short(b.Revision)
	if b.Modified && !strings.HasSuffix(b.Version, "+dirty") {
		// uncommitted changes: the commit no longer describes what ran
		revision += "+dirty"
	}
	return b.Version + " (" + revision + ")"
}

// Long is what --version prints. The Go version and the platform are in it
// because the binary is downloaded prebuilt by an action rather than built by
// the person reading the output, so which build landed on the runner is not
// something they otherwise know.
func (b buildInfo) Long() string {
	var out strings.Builder
	fmt.Fprintf(&out, "contact-sheet %s\n", b.Version)
	if b.Revision != "" {
		revision := b.Revision
		if b.Modified {
			revision += " (uncommitted changes)"
		}
		fmt.Fprintf(&out, "revision: %s\n", revision)
	}
	if b.Time != "" {
		fmt.Fprintf(&out, "built:    %s\n", b.Time)
	}
	fmt.Fprintf(&out, "go:       %s\n", runtime.Version())
	fmt.Fprintf(&out, "platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return out.String()
}
