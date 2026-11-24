# Ryudai Tools

Unofficial tools for University of the Ryukyus students
琉大生のための、非公式の大学生活支援ツール（琉球大学の公式サービスではない）。MCP サーバー名は `ryudai-tools-mcp`。

琉球大学の WebClass LMS から授業資料を取得しやすくする Go 製 Web アプリ。

- 授業資料（PDF等）のダウンロード（コースごとの一括 ZIP も）
- 時間割・コース一覧の表示
- 時間割は今週のダッシュボードから始まる。上に授業中・次の授業と教室の建物の地図（Google マップ、経路案内つき）、下に一週間の時間割表（時刻・教室・休講）
- 講義を押すと、タブで「教務システム」（授業明細スケジュール・シラバスの授業計画・評価基準と評価方法・教科書）と「WebClass」（資料・出席確認）を切り替えられる。授業計画に回番号が明記されていれば各授業回にも内容を表示する
- 履修登録タブは教務の登録入口で期間中と確認できた場合だけ表示する（利用者ごとに最大5分キャッシュ）。期間外は時間割を中心に使える
- 教務ポータルの実日付の予定を月カレンダーで表示（日付ジャンプ、Google カレンダー追加、.ics 出力）
- WebClass のコンテンツは「資料」だけを扱う。試験・レポート・アンケートなどは一覧に出さず、中身の取得・中継もしない
  （開くだけで「開始」扱いになったり提出画面だったりするため。必要なら WebClass で直接開く）
- 出席確認の受付状況の表示
- 教務情報システム（TIGLON）連携
  - シラバス検索（単位数・登録方法・授業形態・概要つき）とシラバス詳細（ログイン不要）
  - 履修登録状況の確認（登録単位 / 履修上限 / 残り単位、空きコマ、希望順位・抽選結果）
  - 空きコマに入れられる講義の横断検索（定員・残席・登録調整方法・時間割との重なり）
  - 履修プランのチェック（コマ被り・単位上限）
  - 成績と卒業要件の見通し（区分ごとの必須単位・修得単位・不足、今期の登録で埋まるか、残り学期で 1 学期あたり何単位必要か、GPA）
  - 上記を AI から使える MCP サーバーと JSON API（読み取り専用。履修登録の追加・削除はしない）

## 開発

### 必要なもの

- mise

### セットアップ

```bash
mise trust
mise install
cp .env.example .env
```

### 起動

```bash
mise run dev
```

（ポートが使われていた場合は既存プロセスを自動で終了してから起動する）

ビルドとテストは `mise run build`、`mise run test`。

確認はコマンドで行い、CI は保険として最小限にしている。

- `mise run check`: push・デプロイ前の確認（`gofmt`、`go vet`、`go test`、Go の構造体と画面の型のずれ、`docs/api.md` と経路の表のずれ、画面の型チェック）。数秒〜十数秒
- `mise run dev-fake`: 偽の WebClass（架空のコースと資料）につないで :8090 で起動する。本物のアカウントなしで画面を確かめられる（ID `e000001` / パスワード `password`。教務システムの機能は使えない）
- `mise run gen-api-doc`: 経路の表（`internal/handler/routes.go`・`internal/ui/routes.go`）から `docs/api.md` を作り直す
- `mise run install-hooks`: `git push` の前に `mise run check` を実行するフックを入れる（クローンごとに 1 回）
- `mise run dist -- v0.2.0`: GitHub Releases と同じ配布アーカイブを手元で作る
- `mise run smoke`: `BASE_URL`（既定 `http://127.0.0.1:8080`）で、期待する版（`EXPECT_VERSION`。既定は手元の `git describe`）が動き、ログインが必要な画面・API が守られ、画面が読み込むファイルが配信されているかを確かめる。例: `BASE_URL=https://ryudai-tools.example.com EXPECT_VERSION=v0.2.0 mise run smoke`
- GitHub Actions（`.github/workflows/ci.yml`）: main への push・pull request で `mise run check` を 1 回だけ実行する

#### 画面（web/）

画面は React + TypeScript + Tailwind CSS + shadcn/ui で `web/` にあり、全ページ（`/courses` `/grades` `/plan` など）を担当する。
パッケージ管理・型チェック（`bun check`）・ビルドには bun（mise で入る）を使い、ビルド結果は `internal/webapp/dist` に出力して `go:embed` でバイナリに埋め込むので、配布するのは今までどおり実行ファイル 1 つだけ。

- ページの URL にはどれも同じ `index.html` を返し、どのページを出すかは画面側（wouter）が決める
- ページのデータは `internal/ui` のハンドラーが `/api/page/...`（ページと同じパス）で JSON を返す。ログインが必要なページで未ログインだと `{"redirect": "/login"}` を返し、画面がログイン画面へ移る。書き込み（POST）は `X-Ryudai-Page` ヘッダーが必要
- 成績・計画は `/api/me/graduation` などの JSON API を直接使う
- ChatGPT 連携の同意画面（`/oauth/authorize`）だけは、CSRF の確認と埋め込み禁止があるので Go が描画する

- `mise run web`: 型チェックとビルド。`mise run build` / `dist` は先にこれを実行する
- `mise run web-dev`: 画面の開発サーバー（`/api` やファイルのダウンロードは `mise run dev` の :8080 に中継）
- `mise run gen-types`: Go の構造体（`internal/ui`・`internal/academic` など）から画面用の TypeScript の型（`web/src/api/gen`）を作る。API の形を変えたら実行する（`mise run check` がずれを検出する）

画面をビルドせずに `go build` した場合も起動はでき、`/app/` は「画面がビルドされていません」と返す。WebClass の検証は `mise run debug -- timetable`。利用できるタスクは `mise tasks` で確認できる。
`.env` は mise が読み込むため、検証 CLI でも認証情報を利用できる。

`http://localhost:8080` でブラウザアクセス。SQLite DB（`./data/ryudai-tools.db`）はアプリが自動で作成する（旧名の `./data/daihack.db` だけがある場合はそちらを使う）。

ログインセッションは最後の利用から7日間保持し、利用するたびに期限を延長する。ブラウザを閉じても期間内はログインを維持し、ログアウトするとセッションを削除する。
公開サーバーはパスワードを保存しない。ログイン時に入力したパスワードは WebClass と教務情報システムへのログインにだけ使い、
保存するのは各システムの Cookie だけ（教務情報システムの Cookie は `kyomu_sessions` テーブル）。Cookie は `CREDENTIAL_KEY`（無ければ `data/` に自動生成される鍵）で暗号化して保存し、
ログインセッションがすべて期限切れになったユーザーの教務情報システムの Cookie は定期的に削除する。他人のパスワードを預からないので、
琉球大学の全学統合ID基盤サービス要項（第三者への貸与・複数人での利用の禁止）に触れる機能は公開サーバーに置かない。

パスワードが必要な機能は、`.env` の `WEBCLASS_USERNAME` / `WEBCLASS_PASSWORD` の本人だけが使える。必要な人は自分でデプロイ・起動する。

- セッションの自動延長（公開サーバー）: WebClass の Cookie の期限が近いとき、またはWebClass・教務情報システム側でセッションが切れたとき、
  `.env` の本人に限って再ログインして読み取りを1回やり直す。他のユーザーはセッションが切れたら再ログインが必要

利用規約は `/terms`、プライバシーポリシーは `/privacy`。

## 本番（Docker）

```bash
cp .env.example .env  # 編集済みの .env があれば不要
docker compose up --build -d
```

データは `./data/` ボリュームに永続化される。

## 設定（.env）

| キー | 説明 | デフォルト |
|---|---|---|
| `PORT` | 待ち受けポート | `8080` |
| `WEBCLASS_BASE_URL` | WebClass のベース URL | `https://webclass.cc.u-ryukyu.ac.jp/webclass` |
| `KYOMU_BASE_URL` | 教務情報システムのポータル URL | `https://tiglon.jim.u-ryukyu.ac.jp/portal` |
| `PUBLIC_BASE_URL` | 外から見たこのアプリの URL。OAuth（ChatGPT 連携）の issuer / resource に使う | リクエストの Host から組み立て |
| `LEGACY_HOSTS` | 旧ドメイン（カンマ区切り）。ここへのアクセスは `PUBLIC_BASE_URL` の同じパスへ 308 で転送する（POST もそのまま） | なし |
| `DB_PATH` | SQLite ファイルパス | `./data/ryudai-tools.db` |
| `PERIOD_1` 〜 `PERIOD_8` | 各時限の開始時刻（任意） | 琉大標準時刻 |

`WEBCLASS_USERNAME` と `WEBCLASS_PASSWORD` を設定すると、指定した本人のアカウントについてのみ、WebClass と教務のセッションを再ログインで更新できます。

## 教務システム連携

成績は「成績照会」から取得する。全成績の一覧は HTML の成績明細（`Results.aspx`）を、区分ごとの必須単位・修得単位・GPA は
HTML に無いため個別成績表の PDF（`ResultsReportPdf.aspx`）を文字の座標から読み取っている。どの科目が必修か（科目単位）は
教務システムに無いので、入学年度別の学生便覧から `get_required_courses` で取得し、`check_my_required_courses` で成績と照合する。

教務情報システムは WebClass と同じ ID・パスワードでログインできるため、Ryudai Tools にログインした時に
同じパスワードで教務情報システムにもログインし、Cookie だけを保存する。セッションが切れたら再ログインが必要（`.env` の本人は自動で再ログインする）。教務システムは ASP.NET WebForms で公開 API が無いので、
画面をスクレイピングして構造化している（`internal/provider/kyomu`）。

### Web UI

- `/registration` 履修登録・確認（時間割画面のタブから切り替え）
- `/registration/candidates` 空きコマに入れられる講義
- `/grades` 成績・卒業要件（区分ごとの不足と今期の見通し、全成績）
- `/syllabus` シラバス検索（ログイン不要）

### MCP（AI 連携）

`/mcp` が MCP サーバー（Streamable HTTP）。シラバス検索は認証無しで使える。
自分の履修情報を使うツールには、次のどちらかで認証する。

- **OAuth（ChatGPT のブラウザ版）**: アプリ追加時に認証「OAuth」を選ぶと Ryudai Tools のログイン画面が開き、
  WebClass の ID・パスワードでログインするだけで連携できる。ChatGPT からの連携では同意画面を省略する。
  連携は設定画面の「連携中のアプリ」から解除できる。実装は `internal/oauth`。
  claude.ai のカスタムコネクタも同じ MCP の認可方式に対応しているはずだが、動作は未確認。
- **API トークン（Codex・Claude Code など）**: 設定画面の「AI 連携（MCP）」で発行したトークンを
  `Authorization: Bearer <トークン>` で送る。

ChatGPT（ブラウザ）:

0. 最初の 1 回だけ、設定 → セキュリティとログイン で「開発者モード」をオンにする
   （OpenAI のドキュメントで必須。アカウントやワークスペースのポリシーによっては使えない）
1. <https://chatgpt.com/plugins> を開き、「＋」から MCP アプリを作成する
2. 名前 `Ryudai Tools`、MCP サーバー URL `https://<ホスト>/mcp`、認証「OAuth」（既定のまま）を入力して作成する
3. Ryudai Tools のログイン画面が開くので WebClass の ID・パスワードでログインする（済みならそのまま完了）
4. 新しいチャットのツールメニューから Ryudai Tools を選んで使う

サーバーのツールを変更・追加したら、chatgpt.com/plugins で Ryudai Tools を開いて「Refresh」を押す（新しい会話から反映）。
利用できるプランは OpenAI のドキュメントに記載が無く未確認（有料プランが必要とする記事もある）。
参考: <https://developers.openai.com/plugins/deploy/connect-chatgpt>

Claude Code:

```bash
claude mcp add --transport http ryudai-tools https://<ホスト>/mcp --header "Authorization: Bearer <トークン>"
```

Codex CLI（トークンは環境変数から読む。`export` は `~/.zshrc` などに書いておく）:

```bash
export RYUDAI_TOOLS_TOKEN="<トークン>"
codex mcp add ryudai-tools --url https://<ホスト>/mcp --bearer-token-env-var RYUDAI_TOOLS_TOKEN
```

`~/.codex/config.toml` に直接書く場合:

```toml
[mcp_servers.ryudai-tools]
url = "https://<ホスト>/mcp"
bearer_token_env_var = "RYUDAI_TOOLS_TOKEN"
```

Codex アプリ（設定 → Plugins → MCP servers → Add server、ターミナル不要）:

| 項目 | 値 |
|---|---|
| Name | `ryudai-tools` |
| Type | `Streamable HTTP` |
| URL | `https://<ホスト>/mcp` |
| ヘッダー名 | `Authorization` |
| ヘッダーの値 | `Bearer <トークン>` |

「Bearer トークン環境変数」欄は環境変数名しか受け付けない（公式ドキュメントの `bearer_token_env_var`）。
環境変数を用意するにはターミナルが要るため、代わりに「ヘッダー（Headers）」欄に
`Authorization` / `Bearer <トークン>` の 1 組を追加する（設定画面のトークン発行後の画面はこの形式でコピーしやすく表示される）。

注意: 公式ドキュメントで確認できているのは `config.toml` の `http_headers`（値をそのまま書ける）の仕様までで、
アプリのフォームの「ヘッダー」欄が同じ挙動かどうかは未確認。フォームで値を入力できない場合は、
`~/.codex/config.toml` に次のように書く（Codex 起動前の環境変数は不要）:

```toml
[mcp_servers.ryudai-tools]
url = "https://<ホスト>/mcp"
http_headers = { "Authorization" = "Bearer <トークン>" }
```

トークン無しでもシラバス・学年暦・時間割・年度を明示した便覧の公開ツールを使えます。本人の登録者数照会・所属に応じた補完・成績照会には認証が必要です。

| ツール | 認証 | 内容 |
|---|---|---|
| `search_syllabus` | 任意 | 軽量シラバス検索（既定10件）。`include_details=true` で単位・概要等、`include_availability=true` で本人の登録者数・残席を追加取得 |
| `get_syllabus` | 任意 | シラバス全文＋公開定員。`include_availability=true` かつ認証時に登録者数・残席・登録調整方法も取得 |
| `list_syllabus_options` | 不要 | 検索条件の選択肢（学部・学科など） |
| `get_my_registration` | 要 | 履修登録状況・残り単位・空きコマ・注意点 |
| `find_available_lectures` | 要 | 空きコマに追加できる講義（定員・残席・登録方法・重なり・前後のコマの教室と休み時間）。`syllabus_keywords` でシラバス全文から絞り込める |
| `describe_lectures` | 任意 | 複数の講義の要点（内容・評価・負担の目安・対象者の注意）をまとめて返す |
| `get_graduation_status` | 要 | 成績と卒業要件の見通し（区分ごとの不足・今期の登録で埋まるか・1 学期あたりの必要単位・未修得の不合格科目）。`include_grades` で全成績 |
| `check_registration_plan` | 要 | コマ被り・単位上限・定員・時間割備考・工学融合のコース別単位の見通し |
| `get_academic_calendar` | 不要 | 学年暦から指定日（既定は日本時間の今日）の学期・クォーター・登録調整/中止・試験・休業・直近の予定・授業振替曜日 |
| `get_course_timetable` | 不要 | 年度・学期別の公式PDFから講義コード・単位・必修/選択・公開定員・曜日時限・対象年次・備考を取得 |
| `get_study_rules` | 任意 | 適用便覧年度別の学部・コースの必要単位、履修条件、卒業研究条件等を出典ページつきで取得。認証時は所属・入学年度を補う |
| `get_required_courses` | 任意 | 工学部の適用便覧年度・コース別に必修・選択必修の科目番号・名称・単位と出典を取得。`all_courses=true` で全コース |
| `check_my_required_courses` | 要 | 本人の学籍情報を使い、必修科目を過去の成績・今期登録と照合。修得済み・履修中・未修得・確認不能を区別 |

MCP の取得量: `search_syllabus` は詳細取得・本人の残席照会を既定では行いません。詳細なしでは未取得の `credits` を省略し、`details_included=false` を返します。`total` と `truncated` で検索結果の省略を確認できます。以前の詳細付き検索は `include_details=true, limit=20` で指定できます。残席照会は `search_syllabus`・`get_syllabus`・`describe_lectures` の `include_availability=true` で有効にします。Web UI の取得方法は変更していません。

候補比較は `describe_lectures`、全文の確認は `get_syllabus` を使います。`find_available_lectures` は既定20件（要約なしは30件）、最大100件で、続きは `offset` を指定します。卒業状況に必修修得状況が含まれている場合は `check_my_required_courses` の重複呼び出しは不要です。

### 学年暦・時間割・便覧を使った履修相談

「今日の授業」「指定日の予定」は `get_my_schedule({"date":"2026-10-22","days":1})`（要認証）で取得できます。`date` を省略すると日本時間の今日、`days` は既定1日・最大31日です。ポータルの週間表示を読み取り、実日付・開始終了時刻・教室・担当教員・休講等の表示を返します。曜日固定の履修一覧から予定を繰り返して生成しません。`days[].events=[]` はその日を取得できて予定がなかったことを示し、取得失敗や形式変更はエラーになります。全学の行事は `get_academic_calendar` で別途確認してください。

時間割画面の「カレンダー」から、月表示で日付を指定できます。各予定の「Google カレンダーに追加」は入力済みの作成画面を開き、本人が保存します。1か月分をまとめて取り込む場合は .ics を保存し、パソコン版Googleカレンダーの「設定 → インポート / エクスポート」で読み込みます。取り込み済みの予定は自動更新されません。「WebClass のコース」タブから資料・出席確認を開けます。

教室表記（工4-211、共2-104、農西(W)203、複405、文講112 など）から建物を引く対応表は `internal/academic/campus.go`。座標は OpenStreetMap の建物データと Google マップで確認したもので、2026年度の公式時間割 PDF に出る教室表記で網羅を確認しています。略称から建物を特定できない教室（国セ・研セ・体 など）は地図を出さず「未登録」と表示します。予定の API は各予定に `location`（建物・階・座標・地図 URL）を付け、Google カレンダーと .ics の場所欄にも建物名を入れます。

講義1件の全授業回は `GET /api/me/lectures/2026/<講義コード>`（MCP は `get_my_lecture_schedule`、CLI は `go run ./cmd/kyomu-debug lecture-schedule <講義コード>`）。教務の「授業明細スケジュール」を読み、ユーザー別に10分間キャッシュします。対応する WebClass コースは `年度下2桁 + S + 講義コード`（例: 2026年度の `123456789` なら `26S123456789`）で探し、見つからなければ科目名で照合します。

JSON API は `GET /api/me/schedule?date=2026-10-22&days=7`（セッションまたは Bearer 認証）。`format=ics` で同じ期間をカレンダーファイルとして返します。大学側への通信は同一ユーザーで直列にし、ViewState の分割フィールドを引き継いで日付ジャンプします。ログイン応答に週間時間割があれば再利用し、1回の週間取得で7日分をユーザー別に5分間キャッシュします。同じ週の同時要求もまとめます。`refresh=1`（MCP は `refresh:true`）で再取得できます。応答は `private, no-store`、取得日時は `fetched_at`、既存キャッシュだけで返したときは `cached:true` です。失敗時に空の予定を返したり、期限切れの予定で代用したりしません。

実接続でのデバッグは `go run ./cmd/kyomu-debug -date 2026-10-22 -days 7 schedule`、日付ジャンプの結合テストは `KYOMU_LIVE=1 go test ./internal/provider/kyomu -run '^TestLiveSchedule$' -v`。CLI は初回と同一期間のキャッシュ取得時間を標準エラーに表示します。

AI には例えば「今の登録期間を確認して、卒業要件に足りない区分と工学融合のコースのまとまりを考慮し、空きコマに入る候補を挙げて」と依頼できます。MCP の instructions は、学年暦 → 本人の履修・成績 → 適用便覧 → 開講時間割・候補 → 履修プラン確認の順で参照するよう案内します。

- `get_academic_calendar({"date":"2026-10-05","days":30})`: 2026年度の後学期・第3クォーター、登録調整期間を確認。年度全体は `include_all=true`。
- `get_course_timetable({"year":2026,"term":"後学期","faculty":"工学部","course":"知能情報コース"})`: 共通科目・工学融合も含む時間割。`lecture_codes` で絞り込み可能。
- `get_study_rules({"enter_year":2024,"faculty":"工学部","course":"知能情報コース","keyword":"工学融合"})`: **開講年度と入学年度を分けて**便覧を読む。編入・転コース等で異なる便覧が適用される場合は、その年度を指定してください。

シラバス系の既存ツール・JSON API・Web画面は、認証済みなら年度・学期・講義コードを照合し、候補ウィンドウを直接照会して定員・登録者数・残席を統合します。教員への確認依頼後などで履修登録を編集できない状態でも照会します。`availability` に `capacity`・`registered`・`seats_left`・`addable`・`source`・`fetched_at` が入ります。未確認の数値は省略し、取得不能・候補未掲載・年度学期不一致などは `status` と `note` で区別します。候補情報のキャッシュはユーザー・年度学期・コマ・検索対象別で5分間です。専門科目は自学部・他学部を先に照会し、共通教育科目は共通教育を先に照会します。未認証時に他の利用者の登録情報は返しません。

`GET /api/me/candidates?slots=火3&scopes=own&full=1&details=basic` は定員超過を含めて候補の人数・登録方法を先に返し、教室・シラバス要約・公式時間割PDFの補足取得を省き、`details_deferred=true` を返します。`details` を省略すると従来どおり補足情報も取得します。各候補の `source` と `fetched_at` は人数情報の取得元と取得日時です。`open` / `registration_open` は履修登録の編集可否であり、教員への確認依頼後も `false` になります。編集不可の場合、候補照会が成功しても `addable` は `false` にします。科目区分はこの候補一覧には含まれません。

追加候補に出ない登録済み科目は、今期の第1希望登録と照合して `availability.registered_by_user=true` を返します。人数照会の候補に見つからない場合は `status=registered_not_listed` とし、登録者数・残席は不明のままにします。定員から登録者数を推定しません。第2希望はこの登録済み表示の対象外です。

`get_graduation_status` と `check_registration_plan` の `curriculum` は、便覧から確認できた工学融合の同一コース条件と、修得済み・今期登録・追加予定のコース別単位を返します。別コースの2単位ずつを同一コースの4単位として合算しません。科目番号で分類できないものは `unclassified` に残します。これは単位の見通しで、先修科目・導入科目・振替・適用便覧の例外も含めた卒業認定の確定判定ではありません。

所属は学籍番号の数字から推測せず、ログイン後の学籍情報ページから学部・学科・コース・入学年度を取得します。`profile.source` と `profile.fetched_at` で根拠と取得日時を確認できます。編入等で適用便覧年度が異なる場合は明示指定してください。

`get_required_courses({"enter_year":2024,"all_courses":true})` は工学部7コースの専門教育の科目表を返します。`check_my_required_courses({})` は本人のコースを自動選択して成績と照合します。同じ結果を成績画面と `get_graduation_status.required_courses` から確認でき、履修プランには追加予定も反映します。照合は開講年度別シラバスの科目番号と必要単位数を使い、同名の別科目や再履修の単位を重複計上しません。取得不能は `unverified` として返します。選択必修の候補は `selective_required` に分け、その全科目の修得を必須とはしません。対象は専門教育の科目表であり、共通教育・専門基礎教育の外国語や先修条件等は便覧本文と既存の区分別単位チェックを併用します。

公開資料は [学年暦](https://www.u-ryukyu.ac.jp/aboutus/calendar/)、[時間割年度一覧](https://rais.skr.u-ryukyu.ac.jp/dc/?page_id=1281)、[便覧年度一覧](https://rais.skr.u-ryukyu.ac.jp/dc/?page_id=665) から取得します。インメモリで24時間キャッシュし、次のアクセス時に更新します。通信失敗時は前回取得日時と `stale=true` を添えて返します。未掲載年度は他年度で代用しません。PDF形式の変更・未掲載の正誤表などは `warnings` に表示されるので、出典確認が必要です。画像だけのPDFには未対応です。

時間割の訂正表は `corrections`（履修候補・プランでは `timetable_corrections`）に本文・ページ・出典を返します。科目一覧へは自動適用していないため、訂正表があればその記載を優先します。学年暦の予定は指定日の年度内、便覧本文は条件に合う20ページまでです。翌年度の日付や本文の `keyword` を指定して続きを取得できます。

授業開始時刻の既定値は公式の授業時間表に合わせ、1限08:30・2限10:20・3限12:50としています（`PERIOD_n` による上書きが優先）。

2024年度は工学部全7コースの必修表を取得済みです。2026年度の工学部便覧はPDFに日本語の文字対応情報が欠けており、現状では必修表を抽出できません。この場合は一覧を取得不能、本人の判定を未確認として返し、別年度の必修表で代用しません。

候補画面は候補・登録人数・残席・コマの重なりを先に表示し、履修条件・教室・時間割の訂正を本人認証つきの `/registration/candidates/details` から続けて取得します。補足の取得失敗は画面に表示します。JavaScriptを使わない場合は「履修条件・教室情報もまとめて表示する」リンクで取得できます。MCP・JSON APIの通常の候補検索は従来どおり補足も含めて返します。公開PDFの解析結果は内容のハッシュでキャッシュし、資料の変更時に解析し直します。

後読み込みのJavaScriptテストは `node internal/ui/testdata/candidates_js_test.cjs` で実行できます。

### JSON API

経路・パラメータ・認証・応答の型の一覧は [docs/api.md](docs/api.md) にあります（経路の表から自動生成し、CI でずれを検出）。

### 検証

```bash
mise run kyomu -- -subject プログラミング search  # シラバス検索
mise run kyomu -- syllabus <講義コード>            # シラバス詳細
mise run kyomu -- overview                        # 履修状況の要約（要 .env の認証情報）
mise run kyomu -- find 月2 木3                      # 指定コマの候補講義
mise run kyomu -- -kw 発音,会話 -syl find 木3         # シラバス全文のキーワードで絞り、要点つき
mise run kyomu -- describe <講義コード>             # 講義の要点（内容・負担の目安・対象者の注意）
mise run kyomu -- graduation                       # 成績と卒業要件の見通し（要 .env の認証情報）
mise run kyomu -- plan <講義コード> <講義コード>     # 履修プランのチェック
mise run test-live                                 # 実サーバーへの結合テスト
```

## 配布とデプロイ

[GitHub Releases](https://github.com/ieyoukan/ryudai-tools/releases/latest) から、macOS・Linux・Windows（amd64 / arm64）用のアーカイブと `checksums.txt` を取得できます。GitHub Actions が `v*` タグのソースを検証し、React 画面を埋め込んだGoバイナリを配布します。アーカイブにはセットアップ用の `.env.example` とライセンスが入ります。

サーバーでは対象環境のアーカイブを展開し、SHA-256を確認してから `ryudai-tools` を起動します。既存環境を更新する場合は、`.env` と SQLite・暗号鍵があるデータディレクトリを保持してください。GitHubへ直接接続できないサーバーには、手元で取得・検証したアーカイブをSSHなどで転送できます。公開サービスの内部サーバー向け運用手順は運営用リポジトリで管理しています。

ソースから構築する場合は `mise run build` または上記のDocker手順を使います。別のリポジトリで変更版を提供する場合、ビルド時に `VITE_SOURCE_REPOSITORY` にソースのリポジトリURL、`VITE_SOURCE_REF` に配信するコミットまたはタグを指定すると、サイトのフッターから対応するソースを開けます。

## ディレクトリ構成

```
cmd/ryudai-tools/   エントリポイント（公開サーバー）
cmd/kyomu-debug/    教務システムスクレイパーの検証 CLI
cmd/fake-webclass/  手元の開発用の偽の WebClass（mise run dev-fake）
internal/
  provider/kyomu/   教務システム（TIGLON）スクレイパー
  academic/         履修支援（シラバス検索キャッシュ・空きコマ候補・プランチェック）
  mcp/              MCP サーバー
  provider/webclass/ WebClass HTML スクレイパー（webclasstest/ はテスト用の偽の WebClass）
  ui/               Web画面用のJSON API
  lms/              WebClass のサービス層（利用者ごとの順番待ち・再ログイン・時間の上限・エラーの文言）
  routes/           経路の表（docs/api.md の生成元）
  db/               SQLite（セッション・設定・OAuth）
  config/           環境変数ロード
  session/          セッションストア
  handler/          JSON API（教務システム・履修計画）
  middleware/        認証ミドルウェア
```

## ライセンス

このプロジェクトのコードは [GNU Affero General Public License v3.0](LICENSE)（`AGPL-3.0-only`）で公開します。同梱の第三者コードには、各ディレクトリに記載されたライセンスが適用されます。
