import { BookOpen, CalendarDays, ChartColumn, ClipboardList, LogIn, Mail, Monitor, Moon, Route, Settings, Sun } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link, useLocation } from 'wouter'

import { useMe, useRegistrationStatus } from '@/api/page'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { setTheme, useTheme, type Theme } from '@/lib/theme'
import { sourceURL } from '@/lib/project'
import { cn } from '@/lib/utils'

// 時間割を先頭に置く。履修登録は期間中だけ出す。メッセージと設定はヘッダーの右。
const nav = [
  { href: '/courses', label: '時間割', icon: CalendarDays },
  { href: '/registration', label: '履修登録', icon: ClipboardList },
  { href: '/grades', label: '成績', icon: ChartColumn },
  { href: '/plan', label: '計画', icon: Route },
  { href: '/syllabus', label: 'シラバス', icon: BookOpen },
]

export function AppShell({ children }: { children: ReactNode }) {
  const [path] = useLocation()
  const { data: me } = useMe()
  const loggedIn = !!me?.Username
  const { data: registration } = useRegistrationStatus(loggedIn)
  const visibleNav = nav.filter((n) => n.href !== '/registration' || registration?.Status?.state === 'open')
  const bare = path.startsWith('/login')
  // 講義ページは時間割の中の画面として扱う
  const navPath = path.startsWith('/lectures/') ? '/courses' : path
  const title = nav.find((n) => navPath.startsWith(n.href))?.label

  return (
    <div className="min-h-svh">
      <a href="#main-content" className="sr-only fixed left-4 top-2 z-50 rounded-md bg-card px-4 py-3 text-sm font-medium focus:not-sr-only">本文へ移動</a>
      <header className="sticky top-0 z-30 border-b bg-card/90 backdrop-blur supports-[backdrop-filter]:bg-card/75">
        <div className="mx-auto flex h-12 max-w-6xl items-center gap-2 px-4 md:h-14">
          <Link href={loggedIn ? '/courses' : '/login'} className="flex shrink-0 items-center gap-2 font-semibold" aria-label="Ryudai Tools">
            <img src="/brand/logo.svg" alt="" width={24} height={24} />
            <span className={cn('text-sm', title && 'hidden md:inline')}>Ryudai Tools</span>
          </Link>
          {title && <span className="text-sm font-semibold md:hidden">{title}</span>}
          {loggedIn && (
            <nav className="ml-4 hidden items-center gap-1 md:flex" aria-label="メニュー">
              {visibleNav.map((n) => (
                <Link
                  key={n.href}
                  href={n.href}
                  aria-current={navPath.startsWith(n.href) ? 'page' : undefined}
                  className="rounded-md px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground aria-[current=page]:bg-muted aria-[current=page]:font-medium aria-[current=page]:text-foreground"
                >
                  {n.label}
                </Link>
              ))}
            </nav>
          )}
          <div className="ml-auto flex items-center gap-0.5">
            <ThemeToggle />
            {loggedIn ? (
              <>
                <IconLink href="/messages" label="メッセージ" active={path.startsWith('/messages')}><Mail /></IconLink>
                <IconLink href="/settings" label="設定" active={path.startsWith('/settings')}><Settings /></IconLink>
              </>
            ) : (
              !bare && (
                <Button asChild size="sm" className="ml-1">
                  <Link href={'/login?next=' + encodeURIComponent(path)}><LogIn />ログイン</Link>
                </Button>
              )
            )}
          </div>
        </div>
      </header>

      <main id="main-content" tabIndex={-1} className={cn('mx-auto max-w-6xl scroll-mt-20 px-4 pt-4 md:py-8', loggedIn ? 'pb-[calc(7rem+env(safe-area-inset-bottom,0px))]' : 'pb-12')}>{children}</main>

      <footer className={cn('mx-auto max-w-6xl px-4 pb-10 text-center text-xs text-muted-foreground', loggedIn && 'hidden md:block')}>
        Ryudai Tools ・ 非公式の大学生活支援ツール（琉球大学の公式サービスではありません） ・{' '}
        <Link href="/terms" className="hover:underline">利用規約</Link> ・ <Link href="/privacy" className="hover:underline">プライバシー</Link> ・{' '}
        <Link href="/downloads" className="hover:underline">ダウンロード</Link> ・{' '}
        <a href={sourceURL} className="hover:underline">ソースコード（GitHub）</a>
      </footer>

      {loggedIn && (
        <nav className="fixed inset-x-0 bottom-0 z-30 border-t bg-card/95 pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden" aria-label="メニュー">
          <div className="grid" style={{ gridTemplateColumns: `repeat(${visibleNav.length}, minmax(0, 1fr))` }}>
            {visibleNav.map((n) => {
              const active = navPath.startsWith(n.href)
              return (
                <Link
                  key={n.href}
                  href={n.href}
                  aria-current={active ? 'page' : undefined}
                  className={cn('flex h-14 flex-col items-center justify-center gap-0.5 text-[11px] text-muted-foreground', active && 'font-medium text-primary')}
                >
                  <n.icon className="size-5" strokeWidth={active ? 2.25 : 1.75} />
                  {n.label}
                </Link>
              )
            })}
          </div>
        </nav>
      )}
    </div>
  )
}

function IconLink({ href, label, active, children }: { href: string; label: string; active?: boolean; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button asChild variant="ghost" size="icon" className={cn(active && 'bg-muted text-foreground')}>
          <Link href={href} aria-label={label} aria-current={active ? 'page' : undefined}>{children}</Link>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

const themeOrder: Theme[] = ['system', 'light', 'dark']
const themeLabel: Record<Theme, string> = { system: '端末の設定に合わせる', light: 'ライト', dark: 'ダーク' }

function ThemeToggle() {
  const { theme } = useTheme()
  const Icon = theme === 'light' ? Sun : theme === 'dark' ? Moon : Monitor
  const next = themeOrder[(themeOrder.indexOf(theme) + 1) % themeOrder.length]
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon" onClick={() => setTheme(next)} aria-label={`表示: ${themeLabel[theme]}（押すと${themeLabel[next]}）`}>
          <Icon />
        </Button>
      </TooltipTrigger>
      <TooltipContent>表示: {themeLabel[theme]}</TooltipContent>
    </Tooltip>
  )
}
