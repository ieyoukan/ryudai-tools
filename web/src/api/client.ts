import { navigate } from 'wouter/use-browser-location'

// Go のサーバー（同じオリジン）の JSON API を呼ぶ。ログインは Cookie のセッション。

export class ApiError extends Error {
  readonly status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

function toLogin(): never {
  navigate('/login?next=' + encodeURIComponent(location.pathname + location.search), { replace: true })
  throw new ApiError(401, 'ログインしてください')
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? { Accept: 'application/json' } : { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 401) {
    // 教務システムのセッション切れも 401 で返るので、メッセージがあればそれを出す
    const msg = await res.json().catch(() => null) as { error?: string } | null
    if (msg?.error?.includes('教務システム')) throw new ApiError(401, msg.error)
    toLogin()
  }
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    throw new ApiError(res.status, (data as { error?: string } | null)?.error ?? `読み込めませんでした（HTTP ${res.status}）`)
  }
  return data as T
}

export const api = {
  get: <T,>(path: string) => request<T>('GET', path),
  post: <T,>(path: string, body: unknown) => request<T>('POST', path, body),
  put: <T,>(path: string, body: unknown) => request<T>('PUT', path, body),
  del: <T,>(path: string) => request<T>('DELETE', path),
}
