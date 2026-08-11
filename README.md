# Contact Sheet

The images a CI run produced, in the pull request comment. A table of
thumbnails, rewritten in place on every push.

## Get Started

Two ways in, and one question decides it: can a pull request on this repository
come from a fork?

### Recommended — two workflows

**This one is fork safe.** A contributor's pull request gets its contact sheet
like anyone else's, and no token that can write to your repository is ever in
the same job as code from the fork. The capture runs where GitHub caps it — a
read-only token, no secrets — and hands over image bytes; the comment is written
by a workflow of yours, off your default branch, which never fetches the fork's
head.

It covers your own branches in the same pass, so this is the only setup a
repository needs, fork or not.

```yaml
# .github/workflows/e2e.yaml — runs the pull request's code
name: E2E
on: pull_request

permissions:
  contents: read   # for the checkout; uploading an artifact needs no permission

jobs:
  capture:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - run: npm run e2e
      - uses: actions/upload-artifact@v4
        if: ${{ always() }}   # a failed run is when the images matter most
        with:
          name: captures
          path: e2e/captures
```

```yaml
# .github/workflows/contact-sheet.yaml — runs yours
name: Contact Sheet
on:
  workflow_run:
    workflows: [E2E]     # matched against the name: above, not the file name
    types: [completed]

permissions:
  actions: read          # reads the artifact off the run that triggered this
  contents: write        # pushes the images to refs/contact-sheet/*
  pull-requests: write   # writes the comment

jobs:
  comment:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/download-artifact@v4
        with:
          name: captures
          path: captures
          run-id: ${{ github.event.workflow_run.id }}
          github-token: ${{ github.token }}

      - uses: miyamo2/contact-sheet@main
        with:
          path: captures
          sha: ${{ github.event.workflow_run.head_sha }}
          status: ${{ github.event.workflow_run.conclusion }}
          allow-fork: 'true'
```

Like every event that is not tied to a code ref, `workflow_run` resolves the
workflow from your default branch — so the pull request that adds these two
files will run `E2E` and no comment will appear. It starts working when it is
merged.

Four things are worth knowing before you copy it:

| | |
| --- | --- |
| `allow-fork` | without it a fork's pull request is skipped, because the action assumes the read-only token a `pull_request` run would have. This job's token is yours, and this is how it says so |
| `sha` | that job stands on your default branch, so `GITHUB_SHA` is not the commit under review. This input is what the status line shows and what the pull request is resolved from |
| no checkout | nothing here fetches the fork's head, and nothing should. If you need a custom template, `actions/checkout` in this workflow gives you *your* default branch, which is where it belongs |
| `status` | `workflow_run.conclusion` is the triggering run's outcome as a whole, not one step's |

[Pull requests from forks](#pull-requests-from-forks) has the reasoning, and
what a fork does and does not get to decide.

### The short way — one step

If nothing will ever arrive from a fork — an internal repository, a personal one
— it is one step after whatever produces the images:

```yaml
- name: Capture
  id: capture
  run: npm run e2e

- uses: miyamo2/contact-sheet@main
  if: ${{ always() }}
  with:
    path: e2e/captures
    status: ${{ steps.capture.outcome }}
```

with the two permissions that job needs:

```yaml
permissions:
  contents: write        # pushes the images to refs/contact-sheet/*
  pull-requests: write   # writes the comment
```

Faced with a fork's pull request this does nothing at all: the token it holds
cannot write, so the action skips rather than collecting the images and failing
on the push. The run stays green, `state` comes back `skipped`, and the reason
is put on the run's page — not only in the log — because a pull request with no
comment on it is otherwise a mystery. There is still no comment.

## What the comment looks like

One heading, one status line, and a folded section per directory. Nothing is
configured above, so this is the whole default:

```markdown
<!-- contact-sheet:default -->
### Contact Sheet

✅ succeeded · commit [`9f1c2ab`](https://github.com/miyamo2/blog/commit/9f1c2ab…) · [run #42](https://github.com/miyamo2/blog/actions/runs/12345678)

<details>
<summary><b>desktop-chromium</b> · 6 images</summary>

| file name | image |
| --- | --- |
| `about-dark` | <img src="https://raw.githubusercontent.com/miyamo2/blog/4c7e0d1…/desktop-chromium/about-dark.png" width="360"> |
| `about-light` | <img src="…/desktop-chromium/about-light.png" width="360"> |

</details>

<sub>Kept on `refs/contact-sheet/pr-7/12345678.1`, outside the default fetch refspec — no clone or pull carries these.</sub>
```

Three things in there are worth knowing:

| | |
| --- | --- |
| `<!-- contact-sheet:default -->` | names the comment. The next run rewrites the comment carrying this marker instead of adding one, and `default` is the template that wrote it |
| ✅ / ❌ | the `status` input, i.e. the job that produced the images — not whether publishing them worked |
| `4c7e0d1…` in the URLs | the commit holding the images, which is not the head commit `9f1c2ab` |

The other two states are a single line. Publishing failed:

```markdown
13 images were collected, but pushing them failed (`…`). They are in the artifacts on [the run](…).
```

Nothing to show:

```markdown
No images under the configured path matched the layout — see [the logs](…).
```

`contact-sheet --print-template` prints the template behind all three.

## Where the images go

A comment can only show an image from a public http(s) URL: GitHub's sanitiser
drops `data:` sources, and an artifact needs a login. So the files have to be
fetchable somewhere, and of the available places this is the only one that costs
nothing:

| | |
| --- | --- |
| a branch | `git fetch` takes `refs/heads/*` by default, so every clone and pull of your repository would carry every image |
| Git LFS | keeps clones small, but storage and bandwidth are metered and deleting the files does not give the quota back |
| release assets | free and unmetered, but a repository with no releases grows a Releases section that exists only to hold screenshots |

Contact Sheet pushes the images to a **custom ref**,
`refs/contact-sheet/pr-<number>/<run>`. `refs/heads/*` and `refs/tags/*` are
where branches and tags live by convention; a ref anywhere else under `refs/` is
an ordinary ref that those conventions simply do not reach. This one is not in
the branch list, not in the Releases tab, and not in the default fetch refspec
`+refs/heads/*:refs/remotes/origin/*`, so no clone or pull carries the images.
`raw.githubusercontent.com` serves them anyway, because it addresses a blob by
commit sha and does not care which ref leads there. GitHub does the same thing
for pull requests, under `refs/pull/*`.

The commit under that ref has no parent, so what gets pushed is the images and
none of your repository's history.

One ref per run, never rewritten, so a comment written months ago still
resolves — the ref is what keeps the objects from being collected. To reclaim
the space of a pull request that no longer matters:

```console
$ git ls-remote origin 'refs/contact-sheet/*'
$ git push origin :refs/contact-sheet/pr-42/12345678.1
```

### Further reading

| | |
| --- | --- |
| [gitrepository-layout](https://git-scm.com/docs/gitrepository-layout) | what lives under `refs/`, and which hierarchies are convention rather than rule |
| [Git Internals — Git References](https://git-scm.com/book/en/v2/Git-Internals-Git-References) | what a ref actually is |
| [git-fetch, "Configured Remote-tracking Branches"](https://git-scm.com/docs/git-fetch#_configured_remote_tracking_branches) | the default refspec — why `refs/heads/*` arrives and nothing else does |
| [git-push, `<refspec>`](https://git-scm.com/docs/git-push#Documentation/git-push.txt-ltrefspecgt) | the `HEAD:refs/…` form, and deleting a ref with a leading colon |

### What this cannot do

**Private repositories.** `raw.githubusercontent.com` serves a private
repository only through a short-lived token URL, which a comment cannot load. On
a private repository the action skips the push and writes a comment saying where
the images are instead. Nothing else about the run changes.

## Pull requests from forks

Why the [recommended setup](#recommended--two-workflows) is two files rather
than one, and why it passes `allow-fork`.

A workflow a fork's pull request triggered holds a read-only `GITHUB_TOKEN`.
GitHub caps it there because that workflow is running the fork's code — and for
the same reason it withholds every secret, so a dedicated PAT or a GitHub App in
`secrets` is not a way round it either. Nothing in that job can push the ref or
write the comment.

What lifts the cap is not a permission but a different job. `workflow_run` runs
the default branch's copy of a workflow, which is yours, so it gets your
repository's token and your secrets. Splitting the run in two puts the fork's
code and the write token in separate jobs that never overlap, and the only thing
that crosses between them is the artifact.

That crossing is safe because an artifact is bytes. What would not be safe is
checking out `workflow_run.head_sha` in the second workflow and building it —
`npm ci` alone is enough, `postinstall` runs — because that puts fork code back
next to the write token, which is the whole thing the split exists to prevent.

### `allow-fork`

The action skips a fork's pull request by default, and collecting the images
only to fail on the push is a worse answer than saying so up front. But that
default is about the token, not the pull request — so a workflow holding one
that _can_ write says so with `allow-fork: true` and gets the comment. The token
has to come from somewhere other than the fork's run: a `workflow_run` job
picking up the artifact, an `issue_comment` command, or a PAT.

Note what the second of those implies. A workflow that checks a fork's head out
is building and running a stranger's code with a token that can write to your
repository, so it needs a gate of its own — a command only a maintainer may
issue, an environment with a required reviewer, or a label. **`allow-fork`
decides whether the comment gets written; it decides nothing about whether
writing it was safe.** The recommended setup earns it differently: it never runs
the fork's code in the job holding the token, so there is nothing to gate.

Only half of what it says can be checked, and that half is. `allow-fork` claims
two things — that the token can write, and that running this code was somebody's
decision — and the first is a question GitHub will answer. So a run that sets it
in a `pull_request` job, where the token never can write, skips and names the
workflows that hold one, instead of collecting the images and stopping on a 403
from `git push`. The second claim is the one that matters and the one nothing
can verify; it stays yours.

Either way the run stays green, `state` comes back `skipped`, and the reason and
the fix go to the run's page and its summary — not only the log, because a pull
request with no comment on it is otherwise a mystery.

### What a fork gets to decide

Images and file names, and nothing else — but it does decide those completely.
The workflow file a `pull_request` run executes is the pull request's own copy,
so the artifact's contents, names and size are the contributor's to choose.

The images are pushed to a ref of yours, which costs storage. The names are
written into the comment, and the contents are published: both are where the
care goes. A file whose name would end the table cell, code span or tag a
template put it in is not collected — see [Names that cannot go in a
comment](#names-that-cannot-go-in-a-comment) — and neither is a symbolic link or
a file whose bytes are not the picture its name promises, so nothing outside the
artifact and nothing that is not an image reaches the ref. See [Files that are
not the picture they claim to
be](#files-that-are-not-the-picture-they-claim-to-be).

## How this compares

These tools differ on one thing: where the images live.

| where the images live | what that costs |
| --- | --- |
| an artifact alone | nothing, but the comment cannot show them; a reviewer downloads a zip |
| a third-party image host, e.g. Imgur | the images are public however private the repository, under someone else's rate limits and retention |
| a branch in your repository | `refs/heads/*` is in the default fetch refspec, so every clone and pull carries every image, indefinitely |
| a hosted visual-regression service | baselines, diffing and approvals, but the images leave your repository and snapshots are billed |
| `refs/contact-sheet/*` — this action | one ref per run, invisible to clones, deleted with a `git push origin :ref` when you want the space back |

[comment-webpage-screenshot](https://github.com/saadmk11/comment-webpage-screenshot)
and [comment-pr-with-images](https://github.com/opengisch/comment-pr-with-images)
are the closest actions; both offer the middle two rows, defaulting to a branch.

Two more differences decide whether this is the right tool:

**It does not take the screenshots.** Those actions capture URLs or HTML files
for you, which is less setup right up until your suite already has a screenshot
step with its own fixtures, auth and viewports. Contact Sheet starts
from a directory that already exists, so Playwright, Cypress, Storybook or a
plotting script all feed it the same way.

**It does not diff them.** No baseline, no approval workflow, nothing fails on
a changed pixel. If you need a build to block on a visual change, use one of
the services above; this puts images where a reviewer can see them and stops
there.

## Choosing which files to collect

Every file that looks like an image — png, jpg, jpeg, gif, webp — and that turns
out to be one is collected, and that is the whole of it unless you say
otherwise. SVG is left out on purpose: GitHub's image proxy will not render one
from a raw URL, so collecting them would put broken cells in the comment.

`layout` narrows that and annotates it. It is one expression, matched against
each file's slash-separated path under `path`, and it does two things:

| | |
| --- | --- |
| filters | a file it does not match is skipped, so a trace or a `.gitkeep` in the same directory is harmless |
| annotates | it lifts pieces of the path out by name and attaches them to the image, for your template to group and order by |

The lifting is done by Go's named capture groups, `(?P<name>...)` — captures
from here on, not the screenshots sitting in `e2e/captures`.

The names are yours. The action reads none of them — there is no `row`, no
`col`, no reserved word. A suite with one project per viewport and a light/dark
sweep might write:

```yaml
layout: '^(?:[^/]+/)?(?P<screen>.+?)(?:-(?P<theme>light|dark))?\.png$'
```

```
captures/desktop-chromium/article-list-light.png  ->  screen=article-list  theme=light   dir=desktop-chromium
captures/mobile-chromium/menu-modal.png           ->  screen=menu-modal    theme=""      dir=mobile-chromium
```

and a template then groups by `dir` and columns by `theme`. Go's regexp syntax
uses `(?P<name>...)`, not `(?<name>...)`.

### Names that cannot go in a comment

One more file is skipped whatever the layout says: one whose path holds a
control character, invalid UTF-8, or any of

```
` | < > " \ [ ]
```

Each of those ends the table cell, code span, tag or link a template wrote the
name into, and starts something else. The log names the files this leaves out,
so they do not go missing quietly.

They are refused rather than escaped because the right escaping depends on where
the template puts the name, and that is the template author's decision, not the
action's. It matters most on [a pull request from a
fork](#pull-requests-from-forks), where the names were chosen by whoever opened
it — a space, a `#`, or a name in any script are all still fine, and are escaped
properly where they land in a URL.

### Files that are not the picture they claim to be

A name is not evidence about contents, and the same pull request that chose the
names chose those too. Two more files are skipped whatever the layout says, for
what they are rather than what they are called:

| | |
| --- | --- |
| a symbolic link | copying one follows it, so whatever it points at — anywhere on the runner, inside the checkout or outside it — is what would be committed and pushed to a public ref |
| contents that are not the picture the extension promises | a `.png` holding two megabytes of something else is published all the same and renders as a broken cell |

So the leading bytes of every collected file are read and held against its
extension: `about-light.png` has to be a PNG. Where a `layout` picks out a file
whose extension this action knows nothing about, the bytes still have to be one
of png, jpeg, gif or webp — a comment showing them has no extension to go on
either.

Both are named in the log, for the same reason a rejected name is: unlike a file
the layout did not match, one of these was meant to be there.

## Writing the comment

The body is a [text/template](https://pkg.go.dev/text/template). Point
`template-files` at your own to replace the built-in one:

```yaml
- uses: miyamo2/contact-sheet@main
  with:
    path: e2e/captures
    template-files: .github/contact-sheet.tmpl
```

### One template, one comment

`template-files` takes a comma-separated list, and each entry writes its own
comment, in the order given. That is how you decide how many comments a run
leaves:

```yaml
    template-files: .github/summary.tmpl,.github/desktop.tmpl,.github/mobile.tmpl
```

Each comment is marked `<!-- <comment-id>:<file name without extension> -->`, so
renaming a template starts a new comment and reordering the list does not
shuffle which comment gets rewritten. Two files with the same base name are an
error rather than a comment that overwrites another. Drop a template from the
list and its comment is deleted on the next run.

A body over GitHub's 65536-character limit is an error naming the template that
overflowed. Nothing is trimmed to fit — the action cannot know which images you
wanted — and the fix is another template.

### Templates you do not have to write

An entry is a path in your checkout or an `https://` URL, so the four in this
repository's [`templates/`](./templates) can be used without copying them:

```yaml
- uses: miyamo2/contact-sheet@main
  with:
    path: e2e/captures
    template-files: https://raw.githubusercontent.com/miyamo2/contact-sheet/main/templates/gallery.tmpl
```

| | |
| --- | --- |
| [`gallery.tmpl`](./templates/gallery.tmpl) | images inline, side by side, one folded section per directory. No table, no captures needed |
| [`flat.tmpl`](./templates/flat.tmpl) | one table of every image, rows labelled by path. Pair with `row-label: path` |
| [`summary.tmpl`](./templates/summary.tmpl) | counts and links, no images. Small enough to sit at the top of a long pull request |
| [`themes.tmpl`](./templates/themes.tmpl) | a row per screen and a column per theme, from a `layout` capturing `screen` and `theme` |

Pin the URL to a tag rather than a branch, the same as the `uses:` line above:
the templates are versioned with the action, and a template ahead of your binary
can call a helper it does not have.

Two things a URL cannot do. It is fetched anonymously — nothing of yours is sent
with the request, `GITHUB_TOKEN` included — so a private URL will not resolve,
and it has to be `https`, because whatever comes back is posted as a comment on
your pull request.

### The context

```go
State       "published" | "publish-failed" | "empty"
Status      "success" | "failure" — the job that produced the images
Title       the title input
Version     the contact-sheet build that wrote the comment
Repository  "owner/repo"
SHA         ShortSHA  CommitURL
Run         .ID  .Number  .Attempt  .URL
Pull        .Number  .URL
Ref         Commit          // only when State is "published"
Images      []Image         // .Path .Dir .Name .Ext .URL .Match
Total
Failure
```

`.Succeeded` and `.Published` are shorthands for the two comparisons templates
make most. An image's `.Match` holds the layout's captures, and `field img "x"`
reads either a built-in field or a capture by the same name.

### Helpers

| | |
| --- | --- |
| `groupBy images "dir"` | splits into `.Key` / `.Images` buckets by any field or capture |
| `filter images "theme" "dark"` | keeps the images whose field equals a value |
| `values images "theme"` | the distinct values of a field, in first-appearance order |
| `orderBy names "a,b"` | sorts, putting the listed names first |
| `table images row col colOrder colDefault` | a Markdown table: one row per `row` value, one column per `col` value. An empty `col` puts everything in one column headed `colDefault` |
| `img image` | one `<img>`, or an em dash when there is no URL |
| `details summary body` | a collapsed `<details>` block |
| `field` · `split` · `join` | one field of one image; string to list; list to string |

### Every template has to handle three states

`State` is `published` only when images were collected *and* pushed. A template
that renders images unconditionally will show broken URLs when the push fails:

```gotemplate
{{ if eq .State "published" }}
{{ range groupBy .Images "dir" }}{{ details .Key (table .Images "name" "" "" "image") }}{{ end }}
{{- else if eq .State "publish-failed" }}
{{ .Total }} images were collected, but publishing them failed (`{{ .Failure }}`).
{{- else }}
No images were produced by this run.
{{- end }}
```

## Checking a template without a token

```console
$ go install github.com/miyamo2/contact-sheet/cmd/contact-sheet@latest
$ contact-sheet --dry-run --path e2e/captures --template-files .github/contact-sheet.tmpl
```

`--dry-run` resolves no pull request, pushes nothing and prints the body it
would have posted, so a template can be iterated on locally in seconds.

## Which binary the action installs

The ref on the `uses:` line decides, and nothing in this repository records a
version for it to read:

| `uses: miyamo2/contact-sheet@…` | |
| --- | --- |
| a release tag, `v1.2.3` | that release's prebuilt binary, checked against the release's `checksums.txt` |
| a branch, `main` | built from the branch's current tip with `go install` |
| a commit sha | built from that commit |

A branch or a commit names no release, so the alternative to building would be
running some other commit's binary against this one's `action.yml` and
templates. Building needs Go on the runner: add
[`actions/setup-go`](https://github.com/actions/setup-go) before the step, or
pin to a tag and get the prebuilt binary instead. A binary already on `PATH` is
left alone, which is how this repository's own workflows test the commit under
review.

Either way the binary is what knows its own version — it reads it out of the
build information Go stamps in ([`runtime/debug`](https://pkg.go.dev/runtime/debug)),
which is the release tag for a tagged build and a pseudo-version naming the
commit for the rest. `contact-sheet --version` prints it, the first line of the
step's log repeats it, and `.Version` hands it to a template.

## Inputs

| input | default | |
| --- | --- | --- |
| `path` | — | directory of images. **Required** |
| `layout` | `` | expression filtering the files and naming their captures; empty collects every image |
| `template-files` | built-in | comma-separated text/template files, one comment each |
| `title` | `Contact Sheet` | heading passed to the template |
| `status` | `success` | outcome of the job that produced the images |
| `comment-id` | `contact-sheet` | namespaces the comments this action owns |
| `ref-namespace` | `refs/contact-sheet` | must be outside `refs/heads/*` |
| `row-label` | `file name` | header of the first column of a `table` |
| `image-width` | `360` | width on each `<img>`; `0` omits it |
| `sha` | `GITHUB_SHA` | commit the images belong to; a `workflow_run` job wants `github.event.workflow_run.head_sha` |
| `pull-number` | resolved from the commit | pull request to comment on |
| `dry-run` | `false` | push nothing, comment nothing |
| `allow-fork` | `false` | comment on a fork's pull request; needs a token that is not the fork's |
| `github-token` | `github.token` | needs `contents: write` and `pull-requests: write` |

## Outputs

`state`, `total`, `ref`, `commit`, `comments`, `pull`.

`state` is the one to branch on:

| | |
| --- | --- |
| `published` | images were collected and pushed; `ref`, `commit` and `comments` are set |
| `publish-failed` | images were collected, the push did not work, and the comment says so |
| `empty` | nothing under `path` matched |
| `skipped` | the run ended without a comment: no pull request on this commit, one that is not open, or one from a fork this token cannot write to |

A `skipped` run is a green one. The reason is in the log, and the fork case —
the one you probably did not mean — also writes a notice and a run summary
naming the fix, because a job that passed and did nothing otherwise looks
exactly like a job that worked. The first three are the states a template sees;
`skipped` ends the run before there is anything to render.

## License

MIT
