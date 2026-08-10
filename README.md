English · [日本語](./README_ja.md)

# Contact Sheet

The images a CI run produced, in the pull request comment. A table of
thumbnails, rewritten in place on every push.

Add the step after whatever produces the images:

```yaml
- name: Capture
  id: capture
  run: npm run e2e

- uses: miyamo2/contact-sheet@v1
  if: ${{ always() }}
  with:
    path: e2e/captures
    status: ${{ steps.capture.outcome }}
```

Then grant the two permissions the job needs:

```yaml
permissions:
  contents: write        # pushes the images to refs/contact-sheet/*
  pull-requests: write   # writes the comment
```

Screenshots are the obvious case, but nothing here is specific to them: the
action takes a directory of images and a rule for arranging them, so
visual-regression diffs, rendered plots and generated diagrams work the same
way. Without it they stay in an artifact — a zip behind a login, which cannot
be opened during a review.

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
| `4c7e0d1…` in the URLs | the orphan commit holding the images, which is not the head commit `9f1c2ab` |

The other two states are a single line. Publishing failed:

```markdown
13 images were collected, but pushing them failed (`…`). They are in the artifacts on [the run](…).
```

Nothing to show:

```markdown
No images under the configured path matched the layout — see [the logs](…).
```

`contact-sheet --print-template` prints the template behind all three. It is a
starting point, not a constraint: the action does not know that a contact sheet
is a table, and neither has your template to.

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

Contact Sheet pushes an orphan commit to **`refs/contact-sheet/pr-<number>/<run>`**,
which is in none of those ways visible: not in the branch list, not in the
Releases tab, and not in the default fetch refspec
(`+refs/heads/*:refs/remotes/origin/*`). Nobody's clone or pull pays for it.

Two facts are what make that work. `GITHUB_TOKEN` with `contents: write` may
push a ref outside `refs/heads/*`, and `raw.githubusercontent.com` addresses a
blob by commit sha — it does not care that the commit is reachable from no
branch. So the images are fetchable by URL while being invisible to everything
that walks branches.

One ref per run, never rewritten, so a comment written months ago still
resolves — the ref is what keeps the objects from being collected. To reclaim
the space of a pull request that no longer matters:

```console
$ git ls-remote origin 'refs/contact-sheet/*'
$ git push origin :refs/contact-sheet/pr-42/12345678.1
```

### Two things this cannot do

**Private repositories.** `raw.githubusercontent.com` serves a private
repository only through a short-lived token URL, which a comment cannot load. On
a private repository the action skips the push and writes a comment saying where
the images are instead. Nothing else about the run changes.

**Pull requests from forks.** A fork's `GITHUB_TOKEN` is read-only: it can
neither push the ref nor write the comment. The action detects this and exits
without doing anything.

## How this compares

These tools differ on one thing: where the images live, because a comment can
only load a public http(s) URL.

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
plotting script all feed it the same way — and the grouping follows your file
names rather than a layout it imposes.

**It does not diff them.** No baseline, no approval workflow, nothing fails on
a changed pixel. If you need a build to block on a visual change, use one of
the services above; this puts images where a reviewer can see them and stops
there.

## Choosing which files to collect

Every file that looks like an image — png, jpg, jpeg, gif, webp — is collected,
and that is the whole of it unless you say otherwise. SVG is left out on
purpose: GitHub's image proxy will not render one from a raw URL, so collecting
them would put broken cells in the comment.

`layout` narrows that and annotates it. It is one expression, matched against
each file's slash-separated path under `path`, and it does two things:

| | |
| --- | --- |
| filters | a file it does not match is skipped, so a trace or a `.gitkeep` in the same directory is harmless |
| annotates | it lifts pieces of the path out by name and attaches them to the image, for your template to group and order by |

The lifting is done by Go's named capture groups, `(?P<name>...)`, called
captures from here on — not the screenshots, which this README also calls
captures where they sit in `e2e/captures`.

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

## Writing the comment

The body is a [text/template](https://pkg.go.dev/text/template). Point
`template-files` at your own to replace the built-in one:

```yaml
- uses: miyamo2/contact-sheet@v1
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
- uses: miyamo2/contact-sheet@v1
  with:
    path: e2e/captures
    template-files: https://raw.githubusercontent.com/miyamo2/contact-sheet/v1/templates/gallery.tmpl
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

The built-in template uses four of them and nothing else:

```gotemplate
{{ range groupBy .Images "dir" }}
{{ details .Key (table .Images "name" "" "" "image") }}
{{ end }}
```

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
| `pull-number` | resolved from the commit | pull request to comment on |
| `dry-run` | `false` | push nothing, comment nothing |
| `github-token` | `github.token` | needs `contents: write` and `pull-requests: write` |

## Outputs

`state`, `total`, `ref`, `commit`, `comments`, `pull`.

## License

MIT
