import { useQueryClient } from '@tanstack/react-query'
import { LockKeyhole } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Link } from 'wouter'

import type { LoginData } from '@/api/gen/ui'
import { postPage, usePage } from '@/api/page'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

const emailError = 'メールアドレスではなく、WebClass のユーザー名（琉大ID）を入力してください。'

/** メールアドレスが入力されたときの扱い。サーバーの webclass.NormalizeUsername と同じ規則（判定はサーバーが行う）。 */
function emailHint(value: string): { id: string } | { error: string } | null {
  const s = value.normalize('NFKC').trim()
  const at = s.lastIndexOf('@')
  if (at < 0) return null
  const local = s.slice(0, at).trim()
  const domain = s.slice(at + 1).trim().toLowerCase()
  if (local && !local.includes('@') && (domain === 'u-ryukyu.ac.jp' || domain.endsWith('.u-ryukyu.ac.jp'))) return { id: local }
  return { error: emailError }
}

export function LoginPage() {
  const search = location.search
  const { data } = usePage<LoginData>('/login' + search, { staleTime: 0 })
  const qc = useQueryClient()
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [username, setUsername] = useState('')
  const hint = emailHint(username)
  const next = data?.Next ?? new URLSearchParams(search).get('next') ?? '/courses'

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (hint && 'error' in hint) {
      // 大学のシステムへ送らずに止める（失敗したログインの試行を増やさない）。理由は入力欄の下に出ている
      setError('')
      ;(e.currentTarget.elements.namedItem('username') as HTMLInputElement | null)?.focus()
      return
    }
    const form = new FormData(e.currentTarget)
    setBusy(true)
    setError('')
    try {
      // 成功するとサーバーが戻り先へのリダイレクトを返し、postPage がそこへ移る
      const res = await postPage<LoginData>('/login', { username: String(form.get('username')), password: String(form.get('password')), next })
      if (res?.Error) setError(res.Error)
      else await qc.invalidateQueries()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto max-w-sm py-4 md:py-10">
      <div className="mb-6 flex flex-col items-center gap-2 text-center">
        <img src="/brand/logo.svg" alt="" width={64} height={64} />
        <h1 className="text-xl font-semibold">Ryudai Tools</h1>
        <p className="text-sm text-muted-foreground">琉大生のための、非公式の大学生活支援ツール</p>
      </div>
      <Card>
        <CardContent>
          {data?.ForOAuth && <p className="mb-4 text-sm text-muted-foreground">AI アプリ（ChatGPT など）と連携するために、WebClass の ID・パスワードでログインしてください。</p>}
          <form className="space-y-4" onSubmit={submit} aria-busy={busy}>
            <div className="space-y-1.5">
              <Label htmlFor="username">ユーザー名（琉大ID）</Label>
              <Input id="username" name="username" autoComplete="username" autoCapitalize="none" autoCorrect="off" spellCheck={false} required className="h-11 md:h-9"
                value={username} onChange={(e) => setUsername(e.target.value)} placeholder="例: e000000" aria-describedby="username-help" aria-invalid={hint !== null && 'error' in hint} />
              <p id="username-help" className={cn('text-xs', hint && 'error' in hint ? 'text-critical' : 'text-muted-foreground')}>
                {hint === null ? 'WebClass にログインするときの ID です。メールアドレスではありません。'
                  : 'error' in hint ? hint.error : `メールアドレスの @ より前の「${hint.id}」でログインします。`}
              </p>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="password">パスワード</Label>
              <Input id="password" name="password" type="password" autoComplete="current-password" required className="h-11 md:h-9" />
            </div>
            {error && <p className="text-sm text-critical" role="alert">{error}</p>}
            <Button type="submit" className="h-11 w-full md:h-9" disabled={busy}>{busy ? 'ログインしています…' : 'ログイン'}</Button>
          </form>
        </CardContent>
      </Card>

      <div className="mt-4 rounded-xl border bg-card p-4 text-sm" role="note">
        <p className="flex items-center gap-2 font-medium"><LockKeyhole className="size-4 text-muted-foreground" />パスワードの扱い</p>
        <ul className="mt-2 list-disc space-y-1 pl-5 text-muted-foreground">
          <li>入力したパスワードは、その場で WebClass と教務情報システムへのログインに使うだけです。</li>
          <li>データベース・ファイル・ログのどこにも<strong className="text-foreground">保存しません</strong>。</li>
          <li>運営者を含め、<strong className="text-foreground">ほかの人が見られる状態にはなりません</strong>。</li>
        </ul>
        <p className="mt-2 text-xs text-muted-foreground">
          保存するのは各システムが発行したログイン Cookie だけです（暗号化して保存し、ログアウトで削除）。セッションが切れたらもう一度ログインしてください。
          詳しくは<Link href="/privacy" className="underline underline-offset-4">プライバシーポリシー</Link>。
        </p>
      </div>
      <p className="mt-4 text-center text-xs text-muted-foreground">
        ログインすると<Link href="/terms" className="underline underline-offset-4">利用規約</Link>と<Link href="/privacy" className="underline underline-offset-4">プライバシーポリシー</Link>に同意したものとみなします。
      </p>
    </div>
  )
}
