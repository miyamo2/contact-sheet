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
    group-order: desktop-chromium,mobile-chromium
    row-label: screen
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

## Laying out your images

One expression decides where every image lands. It is matched against each
file's slash-separated path under `path`, and its named captures place the
image:

| capture | |
| --- | --- |
| `group` | which table the image belongs to. Optional — without it you get one flat table |
| `row` | which line of that table. **Required** |
| `col` | which column of that line. Optional — without it everything lands in `col-default` |

The default handles a Playwright suite with one project per viewport and a
light/dark sweep:

```
^(?P<group>[^/]+)/(?P<row>.+?)(?:-(?P<col>light|dark))?\.png$
```

```
captures/desktop-chromium/article-list-light.png   ->  desktop-chromium | article-list | light
captures/desktop-chromium/article-list-dark.png    ->  desktop-chromium | article-list | dark
captures/mobile-chromium/menu-modal.png            ->  mobile-chromium  | menu-modal   | light
```

Files that do not match are skipped, so a stray trace or `.gitkeep` in the same
directory is harmless. Two files landing on the same group/row/column is an
error rather than a silent drop.

Go's regexp syntax uses `(?P<name>...)`, not `(?<name>...)`.

## Customising the comment

The body is a [text/template](https://pkg.go.dev/text/template). Point
`template-file` at your own to replace it:

```yaml
- uses: miyamo2/contact-sheet@v1
  with:
    path: e2e/captures
    template-file: .github/contact-sheet.tmpl
```

`contact-sheet --print-template` prints the built-in one as a starting point.

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
Columns     []string
Groups      []Group         // .Name  .Columns  .Rows
                            //   Row: .Name  .Cells  .Cell "light"
Total       Omitted  Failure
```

`.Succeeded` and `.Published` are shorthands for the two comparisons templates
make most.

### Helpers

| | |
| --- | --- |
| `table .` | renders a Group as a Markdown table, honouring `row-label` and `image-width` |
| `img url` | one `<img>`, or an em dash when the URL is empty |
| `details summary body` | a collapsed `<details>` block |
| `join list sep` | `strings.Join` |

### Every template has to handle three states

`State` is `published` only when images were collected *and* pushed. A template
that renders images unconditionally will show broken URLs when the push fails:

```gotemplate
{{ if eq .State "published" }}
{{ range .Groups }}{{ details .Name (table .) }}{{ end }}
{{- else if eq .State "publish-failed" }}
{{ .Total }} images were collected, but publishing them failed (`{{ .Failure }}`).
{{- else }}
No images were produced by this run.
{{- end }}
```

`Omitted` is non-zero when rows had to be dropped to fit GitHub's 65536-character
comment limit; say so if it is, or reviewers will think those screens were never
captured.

## Checking a template without a token

```console
$ go install github.com/miyamo2/contact-sheet/cmd/contact-sheet@latest
$ contact-sheet --dry-run --path e2e/captures --template-file .github/contact-sheet.tmpl
```

`--dry-run` resolves no pull request, pushes nothing and prints the body it
would have posted, so a template can be iterated on locally in seconds.

## Inputs

| input | default | |
| --- | --- | --- |
| `path` | — | directory of images. **Required** |
| `layout` | see above | expression placing each image |
| `group-order` | `` | comma-separated group names to sort first |
| `col-order` | `light,dark` | comma-separated column names to sort first |
| `col-default` | first of `col-order` | column for images with no `col` capture |
| `template-file` | built-in | text/template for the body |
| `title` | `Contact Sheet` | heading passed to the template |
| `status` | `success` | outcome of the job that produced the images |
| `comment-id` | `contact-sheet` | identifies the comment to rewrite |
| `ref-namespace` | `refs/contact-sheet` | must be outside `refs/heads/*` |
| `row-label` | `name` | header of each table's first column |
| `image-width` | `360` | width on each `<img>`; `0` omits it |
| `pull-number` | resolved from the commit | pull request to comment on |
| `dry-run` | `false` | push nothing, comment nothing |
| `github-token` | `github.token` | needs `contents: write` and `pull-requests: write` |

## Outputs

`state`, `total`, `ref`, `commit`, `comment-id`, `pull`.

## License

MIT
