[English](./README.md) · 日本語

# Contact Sheet

CIが作った画像を、プルリクエストのコメントにサムネイルの表として並べる。プッシュのたびに同じコメントを書き換える。

画像を作るステップの後ろに、これを足す:

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

ジョブに要る権限は2つ。

```yaml
permissions:
  contents: write        # refs/contact-sheet/* に画像をプッシュする
  pull-requests: write   # コメントを書く
```

スクリーンショットが分かりやすい例なだけで、それ専用ではない。画像の入ったディレクトリと並べ方の規則を渡すだけなので、ビジュアルリグレッションの差分画像でも、描画したグラフでも、生成した図でも同じように動く。使わなければ、それらはアーティファクトの中、つまりログインしないと開けないzipに残ったままレビューが終わる。

## コメントはこう見える

見出しが1行、状態が1行、あとはディレクトリごとの折りたたみセクション。上の例では何も設定していないので、これが既定の全部だ:

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

このうち3箇所は知っておく価値がある:

| | |
| --- | --- |
| `<!-- contact-sheet:default -->` | コメントの名前。次の実行はこの目印を持つコメントを書き換えるので、数が増えない。`default` は書いたテンプレートの名前 |
| ✅ / ❌ | `status` 入力、つまり画像を作ったジョブの結果。公開できたかどうかではない |
| URLの `4c7e0d1…` | 画像を保持するorphan commit。headコミット `9f1c2ab` とは別物 |

残り2つの状態は1行で終わる。プッシュに失敗したとき:

```markdown
13 images were collected, but pushing them failed (`…`). They are in the artifacts on [the run](…).
```

出す画像がなかったとき:

```markdown
No images under the configured path matched the layout — see [the logs](…).
```

3つとも同じテンプレートから出ていて、中身は `contact-sheet --print-template` で読める。ただしこれは出発点であって制約ではない。コンタクトシートが表であることをアクションは知らないので、あなたのテンプレートも知らなくていい。

## 画像はどこに置かれるか

コメントが表示できる画像は、公開されたhttp(s) URLのものだけだ。GitHubのサニタイザは `data:` を落とし、アーティファクトはログインを要求する。つまりファイルはどこかから取得できる状態になければならず、選べる置き場所のうち費用がかからないのはここだけだった:

| | |
| --- | --- |
| ブランチ | `git fetch` は既定で `refs/heads/*` を取る。cloneやpullのたびに全画像が付いてくる |
| Git LFS | cloneは軽いままだが、ストレージも帯域も従量で、消しても枠は戻らない |
| リリースアセット | 無料で従量課金もないが、リリースのないリポジトリに、画像を置くためだけのReleasesタブが生える |

Contact Sheet は `refs/contact-sheet/pr-<number>/<run>` にorphan commitをプッシュする。ブランチ一覧にもReleasesタブにも出ず、既定のfetch refspec (`+refs/heads/*:refs/remotes/origin/*`) にも入らない。誰のcloneもpullも、この分を負担しない。

成立させているのは2つの事実だ。`contents: write` を持つ `GITHUB_TOKEN` は `refs/heads/*` の外へrefをプッシュできる。そして `raw.githubusercontent.com` はblobをコミットのshaで引くので、そのコミットがどのブランチからも辿れなくても構わない。おかげで画像はURLで取得できるのに、ブランチを辿る仕組みからは見えない。

refは1実行につき1つ作り、あとは書き換えない。数か月前のコメントがいまも解決するのはそのためで、オブジェクトが回収されずに残るのもこのrefがあるからだ。用済みのプルリクエストの分を空けたくなったら、こうする:

```console
$ git ls-remote origin 'refs/contact-sheet/*'
$ git push origin :refs/contact-sheet/pr-42/12345678.1
```

### できないことが2つある

**プライベートリポジトリ。** `raw.githubusercontent.com` がプライベートリポジトリを返すのは短命なトークン付きURL経由だけで、コメントからは読み込めない。この場合Actionはプッシュを飛ばし、画像の在り処を書いたコメントを残す。実行の他の部分は変わらない。

**forkからのプルリクエスト。** forkの `GITHUB_TOKEN` は読み取り専用で、refのプッシュもコメントの書き込みもできない。Actionはこれを検出して、何もせずに終了する。

## 似たActionとの違い

この種のツールが分かれるのは1点、画像をどこに置くかだ。コメントが公開URLしか読めない、という制約がそこに効いてくる。

| 画像の置き場所 | 代償 |
| --- | --- |
| アーティファクトのみ | 費用はかからないが、コメントには出せない。レビュアーがzipを落とすことになる |
| 第三者の画像ホスト（Imgurなど） | リポジトリが非公開でも画像は公開になる。レート制限も保持期間も相手のもの |
| リポジトリ内のブランチ | `refs/heads/*` は既定のfetch refspecに入る。cloneもpullも全画像を運び続ける |
| ホスト型のビジュアルリグレッションサービス | ベースライン・差分・承認まで揃うが、画像はリポジトリの外に出て、スナップショットは課金対象になる |
| `refs/contact-sheet/*`（このAction） | 1実行1ref。cloneからは見えず、`git push origin :ref` で消せる |

いちばん近いのは [comment-webpage-screenshot](https://github.com/saadmk11/comment-webpage-screenshot) と [comment-pr-with-images](https://github.com/opengisch/comment-pr-with-images) で、どちらも表の真ん中2つを選べる。既定はブランチだ。

採否を分ける違いは、あと2つある。

**撮影はしない。** 上の2つはURLやHTMLファイルを代わりに撮ってくれる。設定が少なく済むのは確かだが、それはスイートがまだ撮影を持っていないうちの話で、フィクスチャも認証もビューポートも組んだ後では旨みが薄い。Contact Sheet は既にあるディレクトリから始まるので、PlaywrightでもCypressでもStorybookでも作図スクリプトでも、同じように渡せる。グループ分けもこちらが決めた形に従わせず、ファイル名のほうに合わせる。

**差分も取らない。** ベースラインも承認フローもなく、ピクセルが変わってもCIは落ちない。見た目の変化でビルドを止めたいなら上のサービスを使うことになる。こちらは画像をレビュアーの見える場所に置くところまでで終わる。

## どのファイルを集めるか

画像らしい拡張子のファイル — png・jpg・jpeg・gif・webp — は全部集める。何も指定しなければそれで終わりだ。SVGは意図的に外してある。GitHubの画像プロキシがraw URLのSVGを描画しないので、集めても壊れたセルがコメントに並ぶだけになる。

`layout` はこれを絞り、同時に注釈を付ける。式は1つで、`path` 以下にある各ファイルのスラッシュ区切りのパスに照合し、2つの働きをする:

| | |
| --- | --- |
| 絞る | 照合しないファイルは飛ばす。同じディレクトリにtraceや `.gitkeep` があっても害はない |
| 注釈する | 名前付きキャプチャが画像に付く。テンプレートはそれでグループ分けや並べ替えをする |

キャプチャ名は書く人のものだ。アクションはどれも読まないし、`row` も `col` も予約されていない。ビューポートごとにプロジェクトを分けてlightとdarkを撮るスイートなら、たとえばこう書ける:

```yaml
layout: '^(?:[^/]+/)?(?P<screen>.+?)(?:-(?P<theme>light|dark))?\.png$'
```

```
captures/desktop-chromium/article-list-light.png  ->  screen=article-list  theme=light   dir=desktop-chromium
captures/mobile-chromium/menu-modal.png           ->  screen=menu-modal    theme=""      dir=mobile-chromium
```

あとはテンプレートが `dir` でグループを作り、`theme` で列を作ればいい。Goの正規表現は `(?P<name>...)` と書く。`(?<name>...)` ではない。

## コメントを書く

本文は [text/template](https://pkg.go.dev/text/template) だ。`template-files` に自分のファイルを渡せば、組み込みのものと置き換わる:

```yaml
- uses: miyamo2/contact-sheet@v1
  with:
    path: e2e/captures
    template-files: .github/contact-sheet.tmpl
```

### テンプレート1枚がコメント1つ

`template-files` はカンマ区切りのリストを取り、各ファイルが並び順どおりに自分のコメントを書く。1回の実行が残すコメントの数は、これで決まる:

```yaml
    template-files: .github/summary.tmpl,.github/desktop.tmpl,.github/mobile.tmpl
```

各コメントには `<!-- <comment-id>:<拡張子を除いたファイル名> -->` が付く。テンプレートを改名すれば新しいコメントが始まり、リストを並べ替えても書き換え先は入れ替わらない。ベース名が同じファイルが2つあると、片方が他方を上書きする代わりにエラーになる。リストから外したテンプレートのコメントは、次の実行で削除される。

GitHubの65536文字を超えた本文は、どのテンプレートが溢れたかを名指しするエラーになる。収めるための切り捨てはしない。どの画像が要るのかをアクションは知らないからで、直し方はテンプレートをもう1枚足すことだ。

### テンプレートに渡る値

```go
State       "published" | "publish-failed" | "empty"
Status      "success" | "failure" — 画像を作ったジョブの結果
Title       title 入力
Repository  "owner/repo"
SHA         ShortSHA  CommitURL
Run         .ID  .Number  .Attempt  .URL
Pull        .Number  .URL
Ref         Commit          // State が published のときだけ
Images      []Image         // .Path .Dir .Name .Ext .URL .Match
Total
Failure
```

`.Succeeded` と `.Published` は、テンプレートが最もよく書く2つの比較の短縮形だ。画像の `.Match` にはlayoutのキャプチャが入っていて、`field img "x"` は組み込みのフィールドとキャプチャを同じ書き方で読む。

### ヘルパー

| | |
| --- | --- |
| `groupBy images "dir"` | 任意のフィールドやキャプチャで `.Key` / `.Images` のバケットに分ける |
| `filter images "theme" "dark"` | フィールドが値に一致する画像だけ残す |
| `values images "theme"` | フィールドの相異なる値を、最初に現れた順で返す |
| `orderBy names "a,b"` | 並べ替える。列挙した名前が先に来る |
| `table images row col colOrder colDefault` | Markdownのテーブル。`row` の値ごとに1行、`col` の値ごとに1列。`col` が空なら全部が1列に入り、その列の見出しが `colDefault` になる |
| `img image` | `<img>` を1つ。URLがなければem dash |
| `details summary body` | 折りたたんだ `<details>` |
| `field` · `split` · `join` | 画像の1フィールド／文字列をリストへ／リストを文字列へ |

組み込みのテンプレートが使うのは、このうち4つだけだ:

```gotemplate
{{ range groupBy .Images "dir" }}
{{ details .Key (table .Images "name" "" "" "image") }}
{{ end }}
```

### 3つの状態を必ず書き分ける

`State` が `published` になるのは、画像を集められて、かつプッシュできたときだけだ。条件を見ずに画像を出すテンプレートは、プッシュが失敗した実行で壊れたURLを並べることになる:

```gotemplate
{{ if eq .State "published" }}
{{ range groupBy .Images "dir" }}{{ details .Key (table .Images "name" "" "" "image") }}{{ end }}
{{- else if eq .State "publish-failed" }}
{{ .Total }} images were collected, but publishing them failed (`{{ .Failure }}`).
{{- else }}
No images were produced by this run.
{{- end }}
```

## トークンなしでテンプレートを試す

```console
$ go install github.com/miyamo2/contact-sheet/cmd/contact-sheet@latest
$ contact-sheet --dry-run --path e2e/captures --template-files .github/contact-sheet.tmpl
```

`--dry-run` はプルリクエストを解決せず、何もプッシュせず、投稿するはずだった本文を出力する。テンプレートの調整を手元で数秒ずつ回せる。

## 入力

| 入力 | 既定値 | |
| --- | --- | --- |
| `path` | — | 画像の入ったディレクトリ。**必須** |
| `layout` | `` | ファイルを絞り、キャプチャに名前を付ける式。空なら全画像を集める |
| `template-files` | 組み込み | カンマ区切りのtext/templateファイル。1枚がコメント1つ |
| `title` | `Contact Sheet` | テンプレートに渡す見出し |
| `status` | `success` | 画像を作ったジョブの結果 |
| `comment-id` | `contact-sheet` | このアクションが持つコメントの名前空間 |
| `ref-namespace` | `refs/contact-sheet` | `refs/heads/*` の外である必要がある |
| `row-label` | `file name` | `table` の1列目の見出し |
| `image-width` | `360` | 各 `<img>` の幅。`0` で省略 |
| `pull-number` | コミットから解決 | コメントするプルリクエスト |
| `dry-run` | `false` | プッシュもコメントもしない |
| `github-token` | `github.token` | `contents: write` と `pull-requests: write` が要る |

## 出力

`state`、`total`、`ref`、`commit`、`comments`、`pull`。

## ライセンス

MIT
