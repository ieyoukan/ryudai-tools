import { AlertTriangle, Inbox } from 'lucide-react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

// どのページでも使う部品。見た目をそろえるため、ページはこれらを組み合わせて作る。

export function PageHeader({ title, description, actions, back }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; back?: ReactNode }) {
  return (
    <div className="space-y-1">
      {back}
      <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
        <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{title}</h1>
        {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {description && <p className="text-sm text-muted-foreground">{description}</p>}
    </div>
  )
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <Card>
      <CardContent className="flex flex-col items-start gap-2">
        <p className="flex items-center gap-2 text-sm font-medium"><AlertTriangle className="size-4 text-critical" />読み込めませんでした</p>
        <p className="text-sm break-words text-muted-foreground">{message}</p>
        {onRetry && <Button variant="outline" size="sm" onClick={onRetry}>もう一度読み込む</Button>}
      </CardContent>
    </Card>
  )
}

export function EmptyState({ children, icon }: { children: ReactNode; icon?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
      {icon ?? <Inbox className="size-5" />}
      {children}
    </div>
  )
}

export function Notice({ children, tone = 'info', className }: { children: ReactNode; tone?: 'info' | 'warning'; className?: string }) {
  return (
    <div className={cn('flex gap-2 rounded-lg bg-muted px-3 py-2.5 text-sm', className)}>
      {tone === 'warning' && <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />}
      <div className="min-w-0 space-y-1 text-muted-foreground">{children}</div>
    </div>
  )
}

export function LoadingBlock({ rows = 3, label = '読み込んでいます' }: { rows?: number; label?: string }) {
  return (
    <div className="space-y-3" role="status" aria-label={label}>
      {Array.from({ length: rows }, (_, i) => <Skeleton key={i} className="h-16 rounded-xl" />)}
    </div>
  )
}

/** 数字を大きく見せる小さなカード（Cloudflare の概要のような）。 */
export function Stat({ label, value, unit, hint, className }: { label: string; value: ReactNode; unit?: string; hint?: ReactNode; className?: string }) {
  return (
    <div className={cn('rounded-xl border bg-card p-3', className)}>
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 text-2xl font-semibold tracking-tight">
        {value}
        {unit && <span className="ml-1 text-xs font-normal text-muted-foreground">{unit}</span>}
      </p>
      {hint && <p className="mt-0.5 text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

/** 見出しつきの区切り。カードの中で使う。 */
export function SectionTitle({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="text-base font-semibold">{children}</h2>
      {actions}
    </div>
  )
}

/** 日時の表示（Go の 0 値は空にする）。 */
export function formatDate(iso: string | undefined, withTime = true) {
  if (!iso || iso.startsWith('0001-')) return ''
  const d = new Date(iso)
  const md = `${d.getMonth() + 1}/${d.getDate()}`
  return withTime ? `${md} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}` : `${d.getFullYear()}/${md}`
}
