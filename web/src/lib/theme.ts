import { useSyncExternalStore } from 'react'

// 表示テーマ。system は端末の設定（prefers-color-scheme）に合わせる。選んだテーマは localStorage に残す。
export type Theme = 'light' | 'dark' | 'system'

const media = window.matchMedia('(prefers-color-scheme: dark)')
const listeners = new Set<() => void>()

function stored(): Theme {
  const t = localStorage.getItem('theme')
  return t === 'light' || t === 'dark' ? t : 'system'
}

function apply() {
  const t = stored()
  document.documentElement.classList.toggle('dark', t === 'dark' || (t === 'system' && media.matches))
  listeners.forEach((l) => l())
}
media.addEventListener('change', apply)

export function setTheme(t: Theme) {
  if (t === 'system') localStorage.removeItem('theme')
  else localStorage.setItem('theme', t)
  apply()
}

export function useTheme(): { theme: Theme; resolved: 'light' | 'dark' } {
  const theme = useSyncExternalStore((l) => (listeners.add(l), () => listeners.delete(l)), stored)
  const dark = useSyncExternalStore((l) => (listeners.add(l), () => listeners.delete(l)), () => document.documentElement.classList.contains('dark'))
  return { theme, resolved: dark ? 'dark' : 'light' }
}
