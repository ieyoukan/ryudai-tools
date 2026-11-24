package session

import "sync"

var webClassUse sync.Map // username -> *sync.Mutex

// UseWebClass は同じ利用者の WebClass へのアクセスを 1 つずつにする。返り値の関数で解放する。
//
// WebClass はセッションごとに「今いるコース」を 1 つだけ持つ。同じセッションで別のコースを
// 並行して開く（講義ページで同じ授業の 2 コースを同時に読むなど）と、互いのコースに切り替わって
// 資料の一覧が空になったり別のコースの中身が返ったりする。コースに入ってから読み終えるまでを
// この関数で囲む。セッション切れからの再ログインは別のロックで行うので、囲んだ中で起きてもよい。
func UseWebClass(username string) (release func()) {
	mu, _ := webClassUse.LoadOrStore(username, &sync.Mutex{})
	m := mu.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}
