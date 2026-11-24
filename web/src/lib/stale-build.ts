// デプロイで画面のファイル名（ハッシュ）が変わると、前の版を開いたままのタブはページ移動のときに
// もう無い古いファイルを読みに行って失敗する。そのときは 1 回だけ再読み込みして新しい版に切り替える。
const key = 'ryudai-tools:reloaded-for-new-build'
// 直前に再読み込みしたばかりなら、本当に読み込めない状態なので繰り返さない（無限に再読み込みしない）。
const retryAfterMs = 30_000

/** 前の版の画面のファイルが読めなかったとき、まだ試していなければ再読み込みして true を返す。 */
export function reloadForNewBuild(): boolean {
  try {
    const last = Number(sessionStorage.getItem(key) ?? 0)
    if (Date.now() - last < retryAfterMs) return false
    sessionStorage.setItem(key, String(Date.now()))
  } catch {
    // sessionStorage が使えない環境でも 1 回は試す
  }
  location.reload()
  return true
}

/** ページの読み込み（動的 import）が失敗したら、新しい版への再読み込みを試す。 */
export function retryImport<T>(load: () => Promise<T>): () => Promise<T> {
  return () =>
    load().catch((err: unknown) => {
      // 再読み込みするまでの間は読み込み中の表示のままにする
      if (reloadForNewBuild()) return new Promise<T>(() => {})
      throw err
    })
}

// Vite が先読み（modulepreload）に失敗したときも同じように扱う
window.addEventListener('vite:preloadError', (event) => {
  if (reloadForNewBuild()) event.preventDefault()
})
