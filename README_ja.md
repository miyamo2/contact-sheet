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
    group-order: desktop-chromium,mobile-chromium
    row-label: screen
```

ジョブに要る権限は2つ。

```yaml
permissions:
  contents: write        # refs/contact-sheet/* に画像をプッシュする
  pull-requests: write   # コメントを書く
```

スクリーンショットが分かりやすい例なだけで、それ専用ではない。画像の入ったディレクトリと並べ方の規則を渡すだけなので、ビジュアルリグレッションの差分画像でも、描画したグラフでも、生成した図でも同じように動く。使わなければ、それらはアーティファクトの中、つまりログインしないと開けないzipに残ったままレビューが終わる。

## コメントはこう見える

見出しが1行、状態が1行、あとはグループごとの折りたたみブロックが続く。冒頭のワークフローが投稿する本文を、1グループ2行に削り、画像URLを短く省いて示す:

```markdown
<!-- contact-sheet -->
### Contact Sheet

✅ succeeded · commit [`9f1c2ab`](https://github.com/miyamo2/blog/commit/9f1c2ab…) · [run #42](https://github.com/miyamo2/blog/actions/runs/12345678)

<details>
<summary><b>desktop-chromium</b> · 3 rows</summary>

| screen | light | dark |
| --- | --- | --- |
| `about` | <img src="https://raw.githubusercontent.com/miyamo2/blog/4c7e0d1…/desktop-chromium/about-light.png" width="360"> | <img src="…/desktop-chromium/about-dark.png" width="360"> |
| `menu-modal` | <img src="…/desktop-chromium/menu-modal.png" width="360"> | — |

</details>

<sub>Kept on `refs/contact-sheet/pr-7/12345678.1`, outside the default fetch refspec — no clone or pull carries these.</sub>
```

このうち4箇所は知っておく価値がある:

| | |
| --- | --- |
| `<!-- contact-sheet -->` | 次の実行が探す目印。これがあるからコメントは積み上がらず、同じものが書き換わる |
| ✅ / ❌ | `status` 入力、つまり画像を作ったジョブの結果。公開できたかどうかではない |
| URLの `4c7e0d1…` | 画像を保持するorphan commit。headコミット `9f1c2ab` とは別物 |
| `—` | その行にその列の画像がない |

グループは折りたたんであるので、ビューポートを6つ回した実行でもページが埋まらない。`group` キャプチャのないレイアウトなら、テーブルは1つになり、折りたたみも付かない。

残り2つの状態は1行で終わる。プッシュに失敗したとき:

```markdown
13 images were collected, but pushing them failed (`…`). They are in the artifacts on [the run](…).
```

出す画像がなかったとき:

```markdown
No images under the configured path matched the layout — see [the logs](…).
```

3つとも同じテンプレートから出ている。中身は `contact-sheet --print-template` で読める。

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

## 画像の並べ方

1つの正規表現が全画像の行き先を決める。`path` 以下にある各ファイルのスラッシュ区切りのパスに対して照合し、名前付きキャプチャが置き場所になる:

| キャプチャ | |
| --- | --- |
| `group` | どのテーブルに入るか。省略可 — 無ければテーブルは1つになる |
| `row` | そのテーブルの何行目か。**必須** |
| `col` | その行の何列目か。省略可 — これがないレイアウトでは、列は1つになり名前も付かない |

既定値は、ビューポートごとにプロジェクトを分けてlightとdarkを撮るPlaywrightのスイートに合わせてある:

```
^(?P<group>[^/]+)/(?P<row>.+?)(?:-(?P<col>light|dark))?\.(?:png|jpe?g|gif|webp)$
```

```
captures/desktop-chromium/article-list-light.png   ->  desktop-chromium | article-list | light
captures/desktop-chromium/article-list-dark.png    ->  desktop-chromium | article-list | dark
captures/mobile-chromium/menu-modal.png            ->  mobile-chromium  | menu-modal   | light
```

照合しないファイルは飛ばすので、同じディレクトリにtraceや `.gitkeep` が転がっていても害はない。group・row・列の組が重なったときは、黙って片方を捨てるのではなくエラーにする。

Goの正規表現は `(?P<name>...)` と書く。`(?<name>...)` ではない。

## コメントを自分のものに差し替える

本文は [text/template](https://pkg.go.dev/text/template) で書かれている。`template-file` に自分のファイルを渡せば置き換わる:

```yaml
- uses: miyamo2/contact-sheet@v1
  with:
    path: e2e/captures
    template-file: .github/contact-sheet.tmpl
```

`contact-sheet --print-template` が組み込みのテンプレートを出力するので、それを下敷きにするとよい。

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
Columns     []string
Groups      []Group         // .Name  .Columns  .Rows
                            //   Row: .Name  .Cells  .Cell "light"
Total       Omitted  Failure
```

`.Succeeded` と `.Published` は、テンプレートが最もよく書く2つの比較の短縮形だ。

### ヘルパー

| | |
| --- | --- |
| `table .` | Group を Markdown のテーブルにする。`row-label` と `image-width` を反映する |
| `img url` | `<img>` を1つ。URLが空ならem dashを出す |
| `details summary body` | 折りたたんだ `<details>` |
| `join list sep` | `strings.Join` |

### 3つの状態を必ず書き分ける

`State` が `published` になるのは、画像を集められて、かつプッシュできたときだけだ。条件を見ずに画像を出すテンプレートは、プッシュが失敗した実行で壊れたURLを並べることになる:

```gotemplate
{{ if eq .State "published" }}
{{ range .Groups }}{{ details .Name (table .) }}{{ end }}
{{- else if eq .State "publish-failed" }}
{{ .Total }} images were collected, but publishing them failed (`{{ .Failure }}`).
{{- else }}
No images were produced by this run.
{{- end }}
```

`Omitted` には、GitHubの65536文字制限に収めるために落とした行数が入る。ゼロでないならそう書いておく。書かないと、レビュアーはその画面が最初から撮られていないと思う。

## トークンなしでテンプレートを試す

```console
$ go install github.com/miyamo2/contact-sheet/cmd/contact-sheet@latest
$ contact-sheet --dry-run --path e2e/captures --template-file .github/contact-sheet.tmpl
```

`--dry-run` はプルリクエストを解決せず、何もプッシュせず、投稿するはずだった本文を出力する。テンプレートの調整を手元で数秒ずつ回せる。

## 入力

| 入力 | 既定値 | |
| --- | --- | --- |
| `path` | — | 画像の入ったディレクトリ。**必須** |
| `layout` | 上記 | 各画像の置き場所を決める正規表現 |
| `group-order` | `` | 先に並べるグループ名。カンマ区切り |
| `col-order` | `light,dark` | 先に並べる列名。カンマ区切り |
| `col-default` | `col-order` の先頭 | `col` キャプチャがない画像の列。レイアウトに `col` があるときだけ効く |
| `template-file` | 組み込み | 本文の text/template |
| `title` | `Contact Sheet` | テンプレートに渡す見出し |
| `status` | `success` | 画像を作ったジョブの結果 |
| `comment-id` | `contact-sheet` | 書き換える対象のコメントを特定する |
| `ref-namespace` | `refs/contact-sheet` | `refs/heads/*` の外である必要がある |
| `row-label` | `name` | 各テーブルの1列目の見出し |
| `image-width` | `360` | 各 `<img>` の幅。`0` で省略 |
| `pull-number` | コミットから解決 | コメントするプルリクエスト |
| `dry-run` | `false` | プッシュもコメントもしない |
| `github-token` | `github.token` | `contents: write` と `pull-requests: write` が要る |

## 出力

`state`、`total`、`ref`、`commit`、`comment-id`、`pull`。

## ライセンス

MIT
