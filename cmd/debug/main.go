// debug は WebClass スクレイパーの動作を CLI で検証するツール。
//
// 使い方:
//
//	go run ./cmd/debug [-u <username>] [-p <password>] [-base <url>] [-follow=false] <command> [args...]
//
// command:
//
//	raw <path>            WebClass 相対パスの生 HTML を標準出力（デフォルトで JS リダイレクトを追う）
//	timetable             FetchTimetable の JSON 結果
//	contents <courseID>   FetchCourseContents の JSON 結果（資料のみ）
//	files <courseID> <contentID>  FetchContentFiles の JSON 結果
//	attendance <courseID> FetchAttendanceList の JSON 結果
//
// 認証情報の優先順位: -u/-p フラグ > 環境変数 > .env ファイル
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass"
)

const defaultBase = "https://webclass.cc.u-ryukyu.ac.jp/webclass"

func main() {
	dotenv := loadDotEnv(".env")

	envOr := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v := dotenv[key]; v != "" {
			return v
		}
		return fallback
	}

	username := flag.String("u", envOr("WEBCLASS_USERNAME", ""), "WebClass ユーザー名")
	password := flag.String("p", envOr("WEBCLASS_PASSWORD", ""), "WebClass パスワード")
	baseURL := flag.String("base", envOr("WEBCLASS_BASE_URL", defaultBase), "WebClass ベース URL")
	follow := flag.Bool("follow", true, "raw コマンドで JS リダイレクトを追う")
	courseID := flag.String("course", "", "raw コマンドで acs_ トークンを取得するコース ID")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	if *username == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "エラー: -u と -p（または WEBCLASS_USERNAME / WEBCLASS_PASSWORD）が必要です")
		os.Exit(1)
	}

	client, err := webclass.New(*baseURL)
	if err != nil {
		fatalf("クライアント作成失敗: %v", err)
	}
	if _, err := client.Login(*username, *password); err != nil {
		fatalf("ログイン失敗: %v", err)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "raw":
		if len(rest) == 0 {
			fatalf("raw には WebClass 相対パスが必要です")
		}
		doRaw(client, *baseURL, rest[0], *follow, *courseID)

	case "timetable":
		result, err := client.FetchTimetable()
		printJSON(result, err)

	case "contents":
		if len(rest) == 0 {
			fatalf("contents には courseID が必要です")
		}
		result, err := client.FetchCourseContents(rest[0])
		printJSON(result, err)

	case "files":
		if len(rest) < 2 {
			fatalf("files には courseID と contentID が必要です")
		}
		result, err := client.FetchContentFiles(rest[0], rest[1])
		printJSON(result, err)

	case "attendance":
		if len(rest) == 0 {
			fatalf("attendance には courseID が必要です")
		}
		result, err := client.FetchAttendanceList(rest[0])
		printJSON(result, err)

	case "text":
		if len(rest) < 2 {
			fatalf("text には courseID と contentID が必要です")
		}
		result, err := client.FetchContentText(rest[0], rest[1])
		if err != nil {
			fatalf("エラー: %v", err)
		}
		fmt.Println(result)

	default:
		fmt.Fprintf(os.Stderr, "不明なコマンド: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}

// framePriority は FRAMESET から優先して追うフレームの順。
var framePriority = []*regexp.Regexp{
	regexp.MustCompile(`dqstn_answer\.php`),
	regexp.MustCompile(`dqstn_question\.php`),
	regexp.MustCompile(`show_info\.php`),
	regexp.MustCompile(`txtbk_show`),
}
var frameSkip = regexp.MustCompile(`dqstn_button\.php|show_header\.php|show_foot`)

func doRaw(client *webclass.Client, baseURL, path string, follow bool, courseID string) {
	fetchURL := path
	if !strings.HasPrefix(path, "http") {
		base := strings.TrimRight(baseURL, "/")
		fetchURL = base + "/" + strings.TrimLeft(path, "/")
	}

	// courseID が指定された場合はコースセッションを確立して acs_ を URL に付与する
	if courseID != "" && !strings.Contains(fetchURL, "acs_=") {
		if acs := client.CourseACSToken(courseID); acs != "" {
			fmt.Fprintf(os.Stderr, "# acs_ トークン取得: %s\n", acs)
			sep := "?"
			if strings.Contains(fetchURL, "?") {
				sep = "&"
			}
			fetchURL += sep + "acs_=" + acs
		} else {
			fmt.Fprintln(os.Stderr, "# 警告: コースページから acs_ トークンが取得できませんでした")
		}
	}

	const maxHops = 10
	postedToInfo := false
	var body []byte
	var ct string
	var err error
	fetched := false

	for i := 0; i < maxHops; i++ {
		if !fetched {
			body, ct, err = client.FetchBodyAbsCT(fetchURL)
			if err != nil {
				fatalf("取得失敗: %v", err)
			}
		}
		fetched = false

		fmt.Fprintf(os.Stderr, "# [%d] URL: %s\n# Content-Type: %s  Size: %d bytes\n",
			i+1, fetchURL, ct, len(body))

		if !follow || !strings.Contains(ct, "text/html") {
			fmt.Println()
			fmt.Print(string(body))
			return
		}

		// 1. FRAMESET を優先（条件付き JS リダイレクトより先に処理）
		if next := selectFrame(client, body, fetchURL); next != "" {
			fmt.Fprintf(os.Stderr, "# → FRAMESET フレーム: %s\n", next)
			fetchURL = next
			continue
		}

		// 2. show_info.php は「開始」を自動 POST する
		if strings.Contains(fetchURL, "show_info.php") && !postedToInfo {
			postedToInfo = true
			nb, nct, finalURL, nerr := client.StartShowInfo(body, fetchURL)
			if nerr != nil {
				fatalf("show_info POST 失敗: %v", nerr)
			}
			fmt.Fprintf(os.Stderr, "# → show_info.php 自動 POST (next=開始) → %s\n", finalURL)
			body, ct, fetchURL = nb, nct, finalURL
			fetched = true
			continue
		}

		// 3. JS リダイレクトを追う（FRAMESET がない場合）
		next := client.JSRedirect(body, fetchURL)
		if next == "" {
			fmt.Println()
			fmt.Print(string(body))
			return
		}
		if strings.Contains(next, "logout.php") {
			fmt.Fprintln(os.Stderr, "# エラー: logout.php へのリダイレクトを検出。セッションが無効か acs_ トークンが必要です。")
			fmt.Fprintln(os.Stderr, "# ヒント: -course <courseID> フラグを付けると acs_ を自動取得できます。")
			os.Exit(1)
		}
		fetchURL = next
		fmt.Fprintf(os.Stderr, "# → リダイレクト: %s\n", fetchURL)
	}
	fatalf("リダイレクト上限 (%d) を超えました", maxHops)
}

// selectFrame は FRAMESET から優先フレームの絶対 URL を返す。
func selectFrame(client *webclass.Client, body []byte, pageURL string) string {
	frames := client.FrameSrcs(body)
	if frames == nil {
		return ""
	}
	for _, prio := range framePriority {
		for _, src := range frames {
			if prio.MatchString(src) {
				return client.ResolveURL(src, pageURL)
			}
		}
	}
	for _, src := range frames {
		if !frameSkip.MatchString(src) {
			return client.ResolveURL(src, pageURL)
		}
	}
	return ""
}

func printJSON(v any, err error) {
	if err != nil {
		fatalf("エラー: %v", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fatalf("JSON エンコード失敗: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "エラー: "+format+"\n", args...)
	os.Exit(1)
}

func loadDotEnv(path string) map[string]string {
	result := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return result
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(val), `"'`)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "警告: .env 読み込みエラー: %v\n", err)
	}
	return result
}

func usage() {
	fmt.Fprintln(os.Stderr, `使い方: go run ./cmd/debug [フラグ] <command> [args]

コマンド:
  raw <path>            WebClass 相対パスの HTML を表示（JS リダイレクトを自動追跡）
  timetable             FetchTimetable の JSON 結果
  contents <courseID>   FetchCourseContents の JSON 結果（資料のみ）
  files <contentID>     FetchContentFiles の JSON 結果
  attendance <courseID> FetchAttendanceList の JSON 結果
  text <courseID> <contentID>  FetchContentText の結果（教材本文テキスト）

フラグ:
  -u       WebClass ユーザー名（環境変数 WEBCLASS_USERNAME でも可）
  -p       WebClass パスワード（環境変数 WEBCLASS_PASSWORD でも可）
  -base    WebClass ベース URL
  -follow  JS リダイレクトを追う / false で単一リクエストのみ（デフォルト: true）`)
}
