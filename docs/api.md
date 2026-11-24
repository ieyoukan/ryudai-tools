# HTTP API

このファイルは internal/apidoc が経路の表から生成します。直接編集せず、経路の表（internal/handler/routes.go、
internal/ui/routes.go）を直して `mise run gen-api-doc` を実行してください。

- ログイン Cookie: 画面からログインしたときの `ryudai_tools_session`。
- Bearer トークン: 設定画面で発行した API トークン（読み取り専用）を `Authorization: Bearer <トークン>` で送る。
- 応答の型は `web/src/api/gen` の TypeScript の型（Go の構造体から tygo で生成）。
- このほか `GET /api/health`（版の確認）、`/mcp`（AI アプリ向けの MCP。Bearer トークンのみ）、`/oauth/*` と `/.well-known/*`（AI アプリとの連携）があります。

## JSON API

教務システム（TIGLON）・公開資料・履修計画。AI アプリや自作のツールからも使える。

| メソッド | パス | パラメータ | 認証 | 内容 | 応答 |
|---|---|---|---|---|---|
| GET | `/api/academic/calendar` | `date, days, all=1` | 不要 | 学年暦（公式ページから取得。24時間キャッシュ） |  |
| GET | `/api/academic/required` | `enter_year, faculty, course, all_courses=1` | 不要（ログイン中なら本人の情報も使う） | 便覧の必修科目（未ログインなら enter_year が必要） |  |
| GET | `/api/academic/rules` | `enter_year, faculty, course, keyword` | 不要（ログイン中なら本人の情報も使う） | 便覧の履修規程（入学年度・学部・コース） |  |
| GET | `/api/academic/timetable` | `year, term, faculty, course, lecture_codes` | 不要 | 開講時間割（年度・学期・学部・コース） |  |
| GET | `/api/me/candidates` | `slots=月2,木3, scopes=general,own, keywords, with_syllabus=1, offset, limit` | ログイン Cookie または Bearer トークン | 空きコマに入る候補講義 |  |
| GET | `/api/me/graduation` | `include_grades=1` | ログイン Cookie または Bearer トークン | 成績と卒業要件の見通し |  |
| PUT | `/api/me/lecture-categories` |  | ログイン Cookie | 今期の講義の科目区分を自分で選んで保存 |  |
| GET | `/api/me/lectures/{year}/{code}` |  | ログイン Cookie または Bearer トークン | 講義 1 件の全授業回（授業明細スケジュール） |  |
| POST | `/api/me/plan` | `{"lecture_codes": [...]}` | ログイン Cookie または Bearer トークン | 履修プランのチェック |  |
| POST | `/api/me/plan/items` |  | ログイン Cookie | 卒業までの計画に科目を追加 |  |
| DELETE | `/api/me/plan/items` |  | ログイン Cookie | 卒業までの計画と選んだ区分をすべて削除 |  |
| DELETE | `/api/me/plan/items/{id}` |  | ログイン Cookie | 卒業までの計画から科目を 1 件削除 |  |
| GET | `/api/me/plan/suggestions` | `term=2027前期` | ログイン Cookie または Bearer トークン | 卒業までの計画に入れる科目の提案 |  |
| GET | `/api/me/registration` |  | ログイン Cookie または Bearer トークン | 本人の履修登録状況 |  |
| GET | `/api/me/required` | `enter_year` | ログイン Cookie または Bearer トークン | 本人の必修科目の修得状況 |  |
| GET | `/api/me/schedule` | `date, days, format=ics, refresh=1` | ログイン Cookie または Bearer トークン | 本人の実日付の予定（format=ics でカレンダーファイル） |  |
| GET | `/api/syllabus/options` | `faculty` | 不要 | シラバス検索の選択肢（学部・学期など） |  |
| GET | `/api/syllabus/search` | `subject, faculty, term, day, period, limit, details` | 不要（ログイン中なら本人の情報も使う） | シラバス検索 |  |
| GET | `/api/syllabus/{year}/{code}` |  | 不要（ログイン中なら本人の情報も使う） | シラバス詳細 |  |

## ページ API

画面（React）が各ページの表示に使う。GET 以外は `X-Ryudai-Page: 1` ヘッダーが必要。エラーは応答の `Error` に入る。

| メソッド | パス | パラメータ | 認証 | 内容 | 応答 |
|---|---|---|---|---|---|
| GET | `/api/page/courses` |  | ログイン Cookie | WebClass の時間割 | `CoursesData` |
| GET | `/api/page/courses/{courseID}` |  | ログイン Cookie | コースの資料と出席確認 | `ContentsData` |
| GET | `/api/page/courses/{courseID}/contents/{contentID}` |  | ログイン Cookie | 資料 1 件のファイル | `FilesData` |
| GET | `/api/page/login` |  | 不要 | ログイン画面のデータ | `LoginData` |
| POST | `/api/page/login` |  | 不要 | WebClass の ID・パスワードでログイン（大学のメールアドレスは @ より前を ID にする） | `LoginData` |
| POST | `/api/page/logout` |  | ログイン Cookie | ログアウト |  |
| GET | `/api/page/me` |  | 不要（ログイン中なら本人の情報も使う） | ログイン中のユーザー名（画面のヘッダー用） | `BaseData` |
| GET | `/api/page/messages` |  | ログイン Cookie | WebClass の受信箱 | `MessagesData` |
| GET | `/api/page/privacy` |  | 不要 | プライバシーポリシー | `BaseData` |
| GET | `/api/page/registration` |  | ログイン Cookie | 履修登録の状況 | `RegistrationData` |
| GET | `/api/page/registration/candidates` |  | ログイン Cookie | 空きコマの候補講義 | `CandidatesData` |
| GET | `/api/page/registration/status` |  | ログイン Cookie | 履修登録の期間中か（履修登録タブの表示判定） | `RegistrationStatusData` |
| GET | `/api/page/settings` |  | ログイン Cookie | 設定（API トークン・連携中のアプリ） | `SettingsData` |
| POST | `/api/page/settings/api-tokens` |  | ログイン Cookie | API トークンを発行 | `SettingsData` |
| POST | `/api/page/settings/api-tokens/delete` |  | ログイン Cookie | API トークンを削除 | `SettingsData` |
| POST | `/api/page/settings/oauth-grants/revoke` |  | ログイン Cookie | AI アプリとの連携を解除 | `SettingsData` |
| GET | `/api/page/syllabus` |  | 不要 | シラバス検索 | `SyllabusSearchData` |
| GET | `/api/page/syllabus/{year}/{code}` |  | 不要 | シラバス詳細 | `SyllabusDetailData` |
| GET | `/api/page/terms` |  | 不要 | 利用規約 | `BaseData` |

## 資料ファイル

WebClass の資料ファイルの中継とまとめてダウンロード。

| メソッド | パス | パラメータ | 認証 | 内容 | 応答 |
|---|---|---|---|---|---|
| GET | `/courses/{courseID}/files.json` |  | ログイン Cookie | コースの全資料のファイル一覧 |  |
| GET | `/courses/{courseID}/files.zip` |  | ログイン Cookie | コースの全資料を ZIP でダウンロード |  |
| GET | `/courses/{courseID}/summary` |  | ログイン Cookie | AI 要約用に資料の本文をまとめたテキスト |  |
| GET | `/download` | `url, name` | ログイン Cookie | WebClass のファイル 1 件を中継 |  |
