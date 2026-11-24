// kyomu-debug は教務システム（TIGLON）スクレイパーを実サーバに対して検証する CLI。
//
// 使い方:
//
//	go run ./cmd/kyomu-debug [flags] <command> [args...]
//
// ログイン不要:
//
//	options [学部]                       検索フォームの選択肢（学部を渡すと学科も）
//	search                               シラバス検索（-subject -fac -term などで条件指定）
//	syllabus <講義コード>                 シラバス詳細（-year で年度指定）
//	required-catalog                     適用便覧年度別のコース必修一覧（-year、任意で -course）
//	raw <path>                           教務システム相対パスの生 HTML（-login でログイン後に取得）
//
// ログインが必要（読み取りのみ。履修登録の追加・削除は行わない）:
//
//	login                                ログインできるかだけ確認
//	registration                         現在の履修登録状況
//	profile                              学籍情報の所属・入学年度（個人の氏名等は出さない）
//	schedule                             実日付の予定（-date YYYY-MM-DD -days 1〜31、キャッシュ速度も表示）
//	lecture-schedule <講義コード>          講義の全授業回（日付・時刻・教室・担当・備考。-year で年度）
//	required                             本人の必修科目の修得・履修状況
//	candidates <曜日> <時限> [general|own|other]  そのコマに追加できる講義一覧
//
// サービス層（Web UI / API / MCP と同じ処理）:
//
//	digest                               search に単位数などを付けた結果（-limit 件）
//	overview                             履修状況の要約（空きコマ・注意点）
//	find [曜日時限...]                    空きコマ（または指定コマ）に入れられる講義（-scope -filter -full -kw -syl）
//	describe <講義コード...>               講義の要点（内容・負担の目安・履修条件の注意）
//	plan <講義コード...>                   追加したい講義のコマ被り・単位上限チェック
//	graduation                           成績と卒業要件の見通し（区分ごとの不足・今期の登録で埋まるか）
//
// 認証情報は KYOMU_USERNAME / KYOMU_PASSWORD、無ければ WEBCLASS_USERNAME / WEBCLASS_PASSWORD
// （環境変数 > .env）。教務システムと WebClass は同じ ID・パスワード。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
)

func main() {
	dotenv := loadDotEnv(".env")
	envOr := func(fallback string, keys ...string) string {
		for _, k := range keys {
			if v := os.Getenv(k); v != "" {
				return v
			}
			if v := dotenv[k]; v != "" {
				return v
			}
		}
		return fallback
	}

	baseURL := flag.String("base", envOr(kyomu.DefaultBaseURL, "KYOMU_BASE_URL"), "教務システムのベース URL")
	username := flag.String("u", envOr("", "KYOMU_USERNAME", "WEBCLASS_USERNAME"), "ユーザー名")
	password := flag.String("p", envOr("", "KYOMU_PASSWORD", "WEBCLASS_PASSWORD"), "パスワード")
	login := flag.Bool("login", false, "raw の前にログインする")
	limit := flag.Int("limit", 20, "search で表示する最大件数（0 で全件）")
	scopes := flag.String("scope", "", "find の対象（general,own,other をカンマ区切り。省略時 general,own）")
	keyword := flag.String("filter", "", "find の科目名・教員名フィルタ")
	includeFull := flag.Bool("full", false, "find で定員超過の講義も含める")
	sylKeywords := flag.String("kw", "", "find でシラバス全文を絞り込む語（カンマ区切り）")
	withSyllabus := flag.Bool("syl", false, "find で各講義にシラバスの要点を付ける")
	fastCandidates := flag.Bool("fast", false, "find で教室・公式時間割・要約の補足取得を後回しにする")
	scheduleDate := flag.String("date", "", "schedule の開始日（YYYY-MM-DD、省略時は日本時間の今日）")
	scheduleDays := flag.Int("days", 1, "schedule の日数（1〜31）")
	course := flag.String("course", "", "required-catalog のコース名。省略時は学部全体")

	var sp kyomu.SearchParams
	flag.IntVar(&sp.Year, "year", 0, "開講年度（省略時は今年度）")
	flag.StringVar(&sp.Term, "term", "", "学期（前学期/後学期/通年/前期/後期）")
	flag.StringVar(&sp.Faculty, "fac", "", "開講学部（名前またはコード）")
	flag.StringVar(&sp.Department, "dept", "", "開講学科")
	flag.StringVar(&sp.Day, "day", "", "曜日（月〜土/集中）")
	flag.StringVar(&sp.Period, "period", "", "時限（1〜8）")
	flag.StringVar(&sp.Subject, "subject", "", "科目名")
	flag.StringVar(&sp.Instructor, "staff", "", "教員名")
	flag.StringVar(&sp.Keyword, "keyword", "", "全文検索")
	flag.StringVar(&sp.LectureCode, "code", "", "講義コード")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/kyomu-debug [flags] <options|search|syllabus|lecture|raw|login|profile|schedule|lecture-schedule|registration|required|required-catalog|candidates|digest|overview|find|describe|plan|graduation> [args]")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}
	cmd, rest := args[0], args[1:]

	client, err := kyomu.New(*baseURL)
	if err != nil {
		fatalf("クライアント作成失敗: %v", err)
	}
	doLogin := func() {
		if *username == "" || *password == "" {
			fatalf("ログインには -u/-p または KYOMU_USERNAME/KYOMU_PASSWORD（WEBCLASS_* でも可）が必要です")
		}
		start := time.Now()
		if err := client.Login(*username, *password); err != nil {
			fatalf("ログイン失敗: %v", err)
		}
		fmt.Fprintf(os.Stderr, "ログイン成功 (%s)\n", time.Since(start).Round(time.Millisecond))
	}

	start := time.Now()
	defer func() { fmt.Fprintf(os.Stderr, "所要時間: %s\n", time.Since(start).Round(time.Millisecond)) }()

	switch cmd {
	case "schedule":
		svc := academic.New(*baseURL, envPassword(*username, *password))
		result, err := svc.MySchedule(context.Background(), *username, *scheduleDate, *scheduleDays, false)
		if err != nil {
			fatalf("予定取得失敗: %v", err)
		}
		fmt.Fprintf(os.Stderr, "初回取得: %s\n", time.Since(start).Round(time.Millisecond))
		cachedStart := time.Now()
		_, err = svc.MySchedule(context.Background(), *username, *scheduleDate, *scheduleDays, false)
		if err != nil {
			fatalf("キャッシュ取得失敗: %v", err)
		}
		fmt.Fprintf(os.Stderr, "キャッシュ取得: %s\n", time.Since(cachedStart))
		printJSON(result, nil)
	case "lecture-schedule":
		if len(rest) < 1 {
			fatalf("usage: lecture-schedule <講義コード>（-year で年度指定）")
		}
		year := sp.Year
		if year == 0 {
			year = time.Now().Year()
		}
		svc := academic.New(*baseURL, envPassword(*username, *password))
		printJSON(svc.MyLectureSchedule(*username, year, rest[0], false))
	case "options":
		fac := ""
		if len(rest) > 0 {
			fac = rest[0]
		}
		printJSON(client.SyllabusOptions(fac))
	case "required-catalog":
		printJSON(academic.New(*baseURL, nil).University.RequiredCourses(context.Background(), sp.Year, sp.Faculty, *course))

	case "search":
		if sp.IsEmpty() {
			fatalf("search には少なくとも 1 つの条件（-subject, -fac など）を指定してください")
		}
		res, err := client.SearchSyllabus(sp)
		if err != nil {
			fatalf("検索失敗: %v", err)
		}
		fmt.Fprintf(os.Stderr, "%d 件\n", len(res))
		if *limit > 0 && len(res) > *limit {
			res = res[:*limit]
		}
		printJSON(res, nil)

	case "syllabus":
		if len(rest) == 0 {
			fatalf("syllabus には講義コードが必要です")
		}
		printJSON(client.FetchSyllabus(sp.Year, rest[0]))

	case "raw":
		if len(rest) == 0 {
			fatalf("raw にはパスが必要です（例: Public/Syllabus/SearchMain.aspx）")
		}
		if *login {
			doLogin()
		}
		final, body, err := client.Raw(rest[0])
		if err != nil {
			fatalf("取得失敗: %v", err)
		}
		fmt.Fprintf(os.Stderr, "最終 URL: %s (%d bytes)\n", final, len(body))
		fmt.Print(body)

	case "login":
		doLogin()

	case "registration":
		doLogin()
		printJSON(client.FetchRegistration())
	case "profile":
		doLogin()
		printJSON(client.FetchStudentProfile())

	case "candidates":
		if len(rest) < 2 {
			fatalf("candidates には曜日と時限が必要です（例: candidates 月 2 own）")
		}
		period, err := strconv.Atoi(rest[1])
		if err != nil {
			fatalf("時限が数字ではありません: %s", rest[1])
		}
		scope := kyomu.ScopeGeneral
		if len(rest) > 2 {
			scope = kyomu.CandidateScope(rest[2])
		}
		doLogin()
		printJSON(client.FetchCandidates(rest[0], period, scope))

	case "digest":
		if sp.IsEmpty() {
			fatalf("digest には少なくとも 1 つの条件が必要です")
		}
		printJSON(academic.New(*baseURL, nil).SearchSyllabus(sp, *limit, true))

	case "describe":
		if len(rest) == 0 {
			fatalf("describe には講義コードが必要です")
		}
		printJSON(academic.New(*baseURL, nil).DescribeLectures(sp.Year, rest))

	case "overview", "find", "plan", "graduation", "required", "lecture":
		svc := academic.New(*baseURL, envPassword(*username, *password))
		if *limit == 20 && cmd == "find" {
			*limit = 0 // find は既定で全件
		}
		switch cmd {
		case "lecture":
			if len(rest) != 1 {
				fatalf("lecture には講義コードを1つ指定してください")
			}
			printJSON(svc.Lecture(*username, sp.Year, rest[0]))
		case "required":
			printJSON(svc.RequiredCourseStatus(context.Background(), *username, sp.Year))
		case "overview":
			printJSON(svc.Overview(*username))
		case "graduation":
			printJSON(svc.GraduationStatus(*username, false))
		case "find":
			q := academic.CandidateQuery{Keyword: *keyword, IncludeFull: *includeFull, WithSyllabus: *withSyllabus, Limit: *limit, DeferDetails: *fastCandidates}
			for _, kw := range strings.Split(*sylKeywords, ",") {
				if kw = strings.TrimSpace(kw); kw != "" {
					q.SyllabusKeywords = append(q.SyllabusKeywords, kw)
				}
			}
			for _, sc := range strings.Split(*scopes, ",") {
				if sc != "" {
					q.Scopes = append(q.Scopes, kyomu.CandidateScope(sc))
				}
			}
			for _, a := range rest {
				slots := kyomu.ParseSlots(a)
				if len(slots) == 0 {
					fatalf("コマの指定が読めません: %s（例: 月2）", a)
				}
				q.Slots = append(q.Slots, slots...)
			}
			printJSON(svc.FindCandidates(*username, q))
		case "plan":
			if len(rest) == 0 {
				fatalf("plan には講義コードが必要です")
			}
			printJSON(svc.CheckPlan(*username, rest))
		}

	default:
		flag.Usage()
		os.Exit(1)
	}
}

func printJSON(v any, err error) {
	if err != nil {
		fatalf("エラー: %v", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

// loadDotEnv は KEY=VALUE 形式の .env を読む（無ければ空）。
func loadDotEnv(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out
}

// envPassword は -u/-p の認証情報で教務システムにログインする academic.PasswordFunc を返す。
func envPassword(user, pass string) academic.PasswordFunc {
	if user == "" || pass == "" {
		fatalf("このコマンドには -u/-p または KYOMU_USERNAME/KYOMU_PASSWORD（WEBCLASS_* でも可）が必要です")
	}
	return func(username string) (string, bool) { return pass, username == user }
}
