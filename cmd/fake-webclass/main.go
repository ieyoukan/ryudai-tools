// fake-webclass は手元の開発用に、架空のコースと資料を持つ偽の WebClass を起動する。
// 本物のアカウントなしで画面を確かめられる（mise run dev-fake がアプリと一緒に起動する）。
//
//	go run ./cmd/fake-webclass -addr 127.0.0.1:18081
//
// WebClass のベース URL は http://<addr>/webclass。ID e000001・パスワード password でログインできる。
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass/webclasstest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18081", "待ち受けるアドレス")
	delay := flag.Duration("delay", 100*time.Millisecond, "コースに入るときの待ち時間（並行アクセスの確認用）")
	flag.Parse()

	fake := webclasstest.New(map[string]string{"e000001": "password"},
		webclasstest.Course{ID: "26S990000101", Name: "試験プログラミング基礎[2026後期][試験101]", Day: "月", Period: 1,
			Materials: []webclasstest.Material{{ID: "p1", Name: "第1回 講義資料"}, {ID: "p2", Name: "第2回 講義資料"}},
			Other:     []webclasstest.Material{{ID: "p3", Name: "第1回 小テスト", Category: "試験"}, {ID: "p4", Name: "課題1", Category: "レポート"}}},
		webclasstest.Course{ID: "26S990000201", Name: "試験工学[2026後期][試験336]", Day: "木", Period: 2,
			Materials: []webclasstest.Material{{ID: "e1", Name: "講義資料01"}, {ID: "e2", Name: "講義資料02"}}},
		webclasstest.Course{ID: "26S990000202", Name: "試験工学[2026後期][試験985]", Day: "木", Period: 2},
		webclasstest.Course{ID: "26S990000301", Name: "試験英語[2026後期][試験201]", Day: "火", Period: 3},
	)
	fake.Delay = *delay
	mux := http.NewServeMux()
	mux.Handle("/webclass/", fake.Handler())
	log.Printf("偽の WebClass: http://%s/webclass （ID e000001 / パスワード password）", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
