import { useQueryClient } from '@tanstack/react-query'
import { KeyRound, LogOut, Plug } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useLocation } from 'wouter'

import type { SettingsData } from '@/api/gen/ui'
import { postPage, usePage } from '@/api/page'
import { CopyField } from '@/components/CopyField'
import { ErrorState, LoadingBlock, Notice, PageHeader, SectionTitle, formatDate } from '@/components/page'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { setTheme, useTheme, type Theme } from '@/lib/theme'
import { cn } from '@/lib/utils'

export function SettingsPage() {
  const { data, error, isPending, refetch } = usePage<SettingsData>('/settings', { staleTime: 0 })
  const [newToken, setNewToken] = useState('')
  const [, navigate] = useLocation()
  const qc = useQueryClient()

  const logout = async () => {
    await fetch('/api/page/logout', { method: 'POST', credentials: 'same-origin', headers: { 'X-Ryudai-Page': '1' } })
    qc.clear()
    navigate('/login', { replace: true })
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="設定"
        actions={<Button variant="outline" size="sm" onClick={logout}><LogOut />ログアウト</Button>}
        description={data?.Username && `ログイン中: ${data.Username}`}
      />
      {isPending && <LoadingBlock />}
      {error && <ErrorState message={error.message} onRetry={() => refetch()} />}
      {data?.SettingsError && <Notice tone="warning">{data.SettingsError}</Notice>}

      <Card>
        <CardContent className="space-y-3">
          <SectionTitle>表示</SectionTitle>
          <ThemeSwitch />
        </CardContent>
      </Card>

      {data && <Mcp data={data} newToken={newToken} onIssued={(t) => { setNewToken(t); refetch() }} onChanged={() => refetch()} />}
    </div>
  )
}

function ThemeSwitch() {
  const { theme } = useTheme()
  const opts: { v: Theme; label: string }[] = [{ v: 'system', label: '端末に合わせる' }, { v: 'light', label: 'ライト' }, { v: 'dark', label: 'ダーク' }]
  return (
    <div className="flex gap-1 rounded-lg bg-muted p-1" role="radiogroup" aria-label="表示テーマ">
      {opts.map((o) => (
        <button
          key={o.v}
          type="button"
          role="radio"
          aria-checked={theme === o.v}
          onClick={() => setTheme(o.v)}
          className={cn('h-10 flex-1 rounded-md text-sm text-muted-foreground', theme === o.v && 'bg-card font-medium text-foreground shadow-xs')}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

function Mcp({ data, newToken, onIssued, onChanged }: { data: SettingsData; newToken: string; onIssued: (t: string) => void; onChanged: () => void }) {
  const [busy, setBusy] = useState(false)
  const issue = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const name = String(new FormData(e.currentTarget).get('name') ?? '')
    setBusy(true)
    try {
      const res = await postPage<SettingsData>('/settings/api-tokens', { name })
      if (res?.NewToken) onIssued(res.NewToken)
      e.currentTarget?.reset()
    } finally {
      setBusy(false)
    }
  }
  const url = data.MCPURL
  return (
    <Card id="mcp">
      <CardContent className="space-y-5">
        <div className="space-y-2">
          <SectionTitle>AI 連携（MCP）</SectionTitle>
          <p className="text-sm text-muted-foreground">
            Claude や ChatGPT などの AI から、シラバス検索・自分の履修登録状況の確認・空きコマの候補探し・履修プランのチェックができます（読み取り専用）。シラバス検索だけならトークンなしで使えます。
          </p>
          <CopyField value={url} />
        </div>

        <section className="space-y-2">
          <h3 className="flex items-center gap-1.5 text-sm font-medium"><Plug className="size-4 text-muted-foreground" />ChatGPT（ブラウザ）で使う</h3>
          <ol className="list-decimal space-y-1 pl-5 text-sm text-muted-foreground">
            <li>最初の 1 回だけ、ChatGPT の「設定 → セキュリティとログイン」で「開発者モード」をオンにする</li>
            <li><a href="https://chatgpt.com/plugins" target="_blank" rel="noopener" className="underline underline-offset-4">chatgpt.com/plugins</a> を開き、「＋」から MCP アプリを作成する</li>
            <li>名前「Ryudai Tools」と上の URL を入力し、認証は「OAuth」のまま作成する</li>
            <li>Ryudai Tools のログイン画面が開いたら、WebClass の ID・パスワードでログインする</li>
          </ol>
          <p className="text-xs text-muted-foreground">機能が増えたときは、chatgpt.com/plugins で Ryudai Tools を開いて「Refresh」を押すと反映されます。</p>
        </section>

        {(data.OAuthGrants ?? []).length > 0 && (
          <section className="space-y-2">
            <h3 className="text-sm font-medium">連携中のアプリ</h3>
            <ul className="divide-y rounded-lg border">
              {data.OAuthGrants.map((g) => (
                <li key={g.ID} className="flex min-h-12 items-center gap-2 px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm">{g.ClientName}</p>
                    <p className="text-xs text-muted-foreground">連携 {formatDate(g.CreatedAt, false)} ・ 最終利用 {formatDate(g.LastUsedAt) || '未使用'}</p>
                  </div>
                  <ConfirmButton label="解除" title="この連携を解除しますか？" onConfirm={async () => { await postPage('/settings/oauth-grants/revoke', { id: g.ID }); onChanged() }} />
                </li>
              ))}
            </ul>
          </section>
        )}

        <section className="space-y-2">
          <h3 className="flex items-center gap-1.5 text-sm font-medium"><KeyRound className="size-4 text-muted-foreground" />Codex / Claude Code で使う（トークン）</h3>
          {newToken && <NewToken token={newToken} url={url} />}
          <form onSubmit={issue} className="flex gap-2">
            <Input name="name" placeholder="用途（例: Claude Code）" maxLength={40} className="h-10 md:h-9" />
            <Button type="submit" disabled={busy} className="h-10 md:h-9">発行</Button>
          </form>
          {(data.APITokens ?? []).length > 0 && (
            <ul className="divide-y rounded-lg border">
              {data.APITokens.map((t) => (
                <li key={t.ID} className="flex min-h-12 items-center gap-2 px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm">{t.Name} <code className="ml-1 text-xs text-muted-foreground">{t.Prefix}…</code></p>
                    <p className="text-xs text-muted-foreground">発行 {formatDate(t.CreatedAt, false)} ・ 最終利用 {formatDate(t.LastUsedAt) || '未使用'}</p>
                  </div>
                  <ConfirmButton label="無効化" title="このトークンを無効にしますか？" onConfirm={async () => { await postPage('/settings/api-tokens/delete', { id: t.ID }); onChanged() }} />
                </li>
              ))}
            </ul>
          )}
        </section>
      </CardContent>
    </Card>
  )
}

function NewToken({ token, url }: { token: string; url: string }) {
  return (
    <div className="space-y-3 rounded-lg border border-primary/40 bg-primary/5 p-3">
      <p className="text-sm font-medium">トークンを発行しました。この画面を離れると二度と表示されません。</p>
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground">Codex アプリ（設定 → Plugins → MCP servers → Add server）: Name・Type・URL を入れ、「ヘッダー」に 1 行追加</p>
        <dl className="grid grid-cols-[6rem_1fr] items-center gap-1.5 text-xs">
          <dt className="text-muted-foreground">Name</dt><dd><CopyField value="ryudai-tools" /></dd>
          <dt className="text-muted-foreground">Type</dt><dd><CopyField value="Streamable HTTP" /></dd>
          <dt className="text-muted-foreground">URL</dt><dd><CopyField value={url} /></dd>
          <dt className="text-muted-foreground">ヘッダー名</dt><dd><CopyField value="Authorization" /></dd>
          <dt className="text-muted-foreground">ヘッダーの値</dt><dd><CopyField value={`Bearer ${token}`} /></dd>
        </dl>
      </div>
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground">Codex の ~/.codex/config.toml に書く場合</p>
        <CopyField block value={`[mcp_servers.ryudai-tools]\nurl = "${url}"\nhttp_headers = { "Authorization" = "Bearer ${token}" }`} />
      </div>
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground">Claude Code（ターミナル）</p>
        <CopyField block value={`claude mcp add --transport http ryudai-tools ${url} --header "Authorization: Bearer ${token}"`} />
      </div>
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground">Codex CLI（ターミナル）</p>
        <CopyField block value={`export RYUDAI_TOOLS_TOKEN="${token}"\ncodex mcp add ryudai-tools --url ${url} --bearer-token-env-var RYUDAI_TOOLS_TOKEN`} />
      </div>
    </div>
  )
}

function ConfirmButton({ label, title, onConfirm }: { label: string; title: string; onConfirm: () => Promise<void> }) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild><Button size="sm" variant="ghost" className="text-muted-foreground">{label}</Button></DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>元に戻せません。</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <DialogClose asChild><Button variant="outline">やめる</Button></DialogClose>
          <Button variant="destructive" disabled={busy} onClick={async () => { setBusy(true); await onConfirm(); setBusy(false); setOpen(false) }}>{label}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
