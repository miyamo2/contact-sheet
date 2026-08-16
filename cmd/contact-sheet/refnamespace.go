package main

import (
	"errors"
	"fmt"
	"strings"
)

// reserved is a hierarchy under refs/ that --ref-namespace must not name, and
// the reason to hand back when it does.
type reserved struct {
	prefix string
	reason string
}

// The first two are the ones somebody reaches for, and the whole design of this
// action is an argument against them, so each gets the argument back rather than
// a rule. The rest have an owner: git or the forge writes its own refs there and
// expects to be the only one doing it, so a push under one is refused,
// rewritten, or worse, taken and acted on.
var reservedNamespaces = []reserved{
	{"refs/heads", "that is where branches live, and git fetch takes refs/heads/* by default, " +
		"so every clone and pull of the repository would carry every image and keep carrying it, " +
		"the one outcome this action exists to avoid"},
	{"refs/tags", "that is where tags live; git fetches those by default too, and one tag per run " +
		"would bury the repository's tag list under screenshots"},
	{"refs/remotes", "git keeps remote-tracking branches there"},
	{"refs/notes", "git keeps notes there"},
	{"refs/replace", "git keeps replacement objects there, and a ref there changes which commit another one resolves to"},
	{"refs/stash", "git keeps the stash there"},
	{"refs/bisect", "git keeps bisect state there"},
	{"refs/rewritten", "git keeps rebase state there"},
	{"refs/worktree", "git keeps per-worktree refs there"},
	{"refs/pull", "GitHub maintains the pull request refs there"},
	{"refs/pull-requests", "Bitbucket maintains the pull request refs there"},
	{"refs/merge-requests", "GitLab maintains the merge request refs there"},
	{"refs/merge_requests", "GitLab maintains the merge request refs there"},
	{"refs/keep-around", "GitLab keeps its own refs there"},
	{"refs/environments", "GitLab keeps its own refs there"},
	{"refs/pipelines", "GitLab keeps its own refs there"},
	{"refs/changes", "Gerrit maintains the change refs there"},
	{"refs/for", "Gerrit reads a push there as a change proposed for review"},
	{"refs/meta", "Gerrit keeps its own configuration there"},
}

// suggestion is the default, and the shape of an answer for anybody who has
// been told their value is not one.
const suggestion = "refs/contact-sheet"

// checkRefNamespace validates --ref-namespace and returns it with any trailing
// slash and surrounding space taken off, which is the form the ref is composed
// from.
//
// This runs before anything is collected, and that is the point of it. The
// value is not touched again until the push, by which time the images have been
// gathered, copied and committed; a namespace git will not take surfaces there
// as a failed `git push`, which the run reports as `publish-failed` and reads
// like a network problem rather than a bad input.
//
// The composed ref is `<namespace>/pr-<number>/<run id>.<attempt>`, so
// validating the namespace alone is validating the ref: every component
// appended to it is generated here and is a name git accepts. Checking the
// namespace as though it were a whole ref is a shade stricter than git would be
// on the composed one, since it refuses a namespace ending in a dot where the
// composed ref would not end there, and a namespace ending in a dot is no one's
// intention.
func checkRefNamespace(value string) (string, error) {
	const flag = "--ref-namespace"

	namespace := strings.TrimSuffix(strings.TrimSpace(value), "/")
	switch {
	case namespace == "":
		return "", fmt.Errorf("%s: is empty; the images are pushed to <namespace>/pr-<number>/<run>, "+
			"and the default namespace is %s", flag, suggestion)
	case namespace == "refs":
		return "", fmt.Errorf("%s: refs is the root every ref in the repository sits under, "+
			"not a hierarchy to push into; name one inside it, e.g. %s", flag, suggestion)
	case !strings.HasPrefix(namespace, "refs/"):
		return "", fmt.Errorf("%s: %q is not under refs/; the images go to a full ref name, "+
			"which has to start there, e.g. %s", flag, value, suggestion)
	}

	for _, r := range reservedNamespaces {
		if namespace == r.prefix || strings.HasPrefix(namespace, r.prefix+"/") {
			return "", fmt.Errorf("%s: %q is under %s/*: %s; push these somewhere nothing else "+
				"claims, e.g. %s", flag, value, r.prefix, r.reason, suggestion)
		}
	}

	if err := checkRefFormat(namespace); err != nil {
		return "", fmt.Errorf("%s: %q %w, so git would refuse the ref", flag, value, err)
	}
	return namespace, nil
}

// checkRefFormat applies the rules of git-check-ref-format(1) to a whole ref
// name, in Go rather than by running git: this is checked before a repository
// exists to run it in, and the point of checking at all is a message that says
// which rule was broken rather than the exit status of a push.
//
// The errors read as a continuation of "<value> ..." so the caller can quote
// the value the workflow wrote and say what is wrong with it in one line.
//
// One rule is left to the caller: git refuses a one-level name unless asked
// with --allow-onelevel, and refusing "refs" or "contact-sheet" for the shape
// of the name says less than refusing them for what they are.
func checkRefFormat(ref string) error {
	switch {
	case ref == "":
		return errors.New("is empty")
	case strings.HasPrefix(ref, "/"), strings.HasSuffix(ref, "/"), strings.Contains(ref, "//"):
		return errors.New("has an empty path component (a leading, trailing or doubled /)")
	case strings.Contains(ref, ".."):
		return errors.New(`contains ".."`)
	case strings.Contains(ref, "@{"):
		return errors.New(`contains "@{"`)
	case strings.HasSuffix(ref, "."):
		return errors.New("ends with a dot")
	case ref == "@":
		return errors.New(`is "@", which git reads as HEAD`)
	}

	for _, r := range ref {
		switch {
		case r < 0o40 || r == 0o177:
			return fmt.Errorf("contains a control character (%#U)", r)
		case r == ' ':
			return errors.New("contains a space")
		case strings.ContainsRune(`~^:?*[\`, r):
			return fmt.Errorf("contains %q", r)
		}
	}

	for _, component := range strings.Split(ref, "/") {
		switch {
		case strings.HasPrefix(component, "."):
			return fmt.Errorf("has a component beginning with a dot (%q)", component)
		case strings.HasSuffix(component, ".lock"):
			return fmt.Errorf("has a component ending with .lock (%q)", component)
		}
	}
	return nil
}
