import { useQuery, useQueryClient } from '@tanstack/react-query'
import { navigate } from 'wouter/use-browser-location'

import { ApiError } from './client'
import type { BaseData, RegistrationStatusData } from './gen/ui'

// ページのデータ（Go の internal/ui のハンドラーが JSON で返す）を読む。
// ログインが必要なページで未ログインだと {"redirect": "/login"} が返るので、戻り先つきでログイン画面へ移る。

type Redirect = { redirect: string }

function isRedirect(v: unknown): v is Redirect {
  return typeof v === 'object' && v !== null && 'redirect' in v
}

/** サーバーからの移動の指示に従う。ログイン画面へ送られたら、今のページを戻り先にする。 */
export function follow(to: string) {
  if (to === '/login' || to.startsWith('/login?')) {
    const here = location.pathname + location.search
    const next = new URLSearchParams(to.split('?')[1] ?? '').get('next') ?? (here.startsWith('/login') ? '/courses' : here)
    navigate('/login?next=' + encodeURIComponent(next), { replace: true })
    return
  }
  if (/^https?:\/\//.test(to) || to.startsWith('/oauth/')) {
    location.href = to // ほかのサイトや、サーバーが描く画面（OAuth の同意画面）
    return
  }
  navigate(to)
}

async function getPage<T>(path: string): Promise<T> {
  const res = await fetch('/api/page' + path, { credentials: 'same-origin', headers: { Accept: 'application/json' } })
  const data = await res.json().catch(() => null)
  if (isRedirect(data)) {
    follow(data.redirect)
    throw new ApiError(401, '移動しています')
  }
  if (!res.ok && data === null) throw new ApiError(res.status, `読み込めませんでした（HTTP ${res.status}）`)
  return data as T
}

/** ページのデータ。path は /courses のようなページの URL（クエリつき）。 */
export function usePage<T extends BaseData>(path: string, opts: { enabled?: boolean; staleTime?: number; refetchInterval?: number } = {}) {
  return useQuery({
    queryKey: ['page', path],
    queryFn: () => getPage<T>(path),
    enabled: opts.enabled ?? true,
    staleTime: opts.staleTime ?? 30_000,
    refetchInterval: opts.refetchInterval,
    retry: false,
  })
}

/** ページへの送信（ログイン・トークンの発行など）。フォームの値を送り、応答がページのデータなら返す。 */
export async function postPage<T>(path: string, form: Record<string, string> = {}): Promise<T | null> {
  const res = await fetch('/api/page' + path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded', 'X-Ryudai-Page': '1' },
    body: new URLSearchParams(form),
  })
  const data = await res.json().catch(() => null)
  if (isRedirect(data)) {
    follow(data.redirect)
    return null
  }
  if (!res.ok) throw new ApiError(res.status, (data as { error?: string } | null)?.error ?? `送信できませんでした（HTTP ${res.status}）`)
  return data as T
}

/** ログイン中のユーザー（ヘッダーの表示用）。 */
export function useMe() {
  return usePage<BaseData>('/me', { staleTime: 5 * 60_000 })
}

/** 教務の入口で判定した登録期間。履修一覧や成績は取得しない。 */
export function useRegistrationStatus(enabled: boolean) {
  return usePage<RegistrationStatusData>('/registration/status', { enabled, staleTime: 5 * 60_000, refetchInterval: 5 * 60_000 })
}

export function useInvalidatePages() {
  const qc = useQueryClient()
  return () => qc.invalidateQueries({ queryKey: ['page'] })
}
