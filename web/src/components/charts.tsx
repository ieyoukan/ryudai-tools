import type { CSSProperties, ReactNode } from 'react'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { credits } from '@/lib/format'
import { cn } from '@/lib/utils'

// 単位のグラフの部品。色 = 区分、塗り方 = 修得（塗りつぶし）・今期の登録（薄い塗り）・計画（斜線）。
// 塗りの間は面の色の 2px の隙間で区切り、端（基準線の反対側）だけ 4px 丸める。

export type Fill = 'earned' | 'registered' | 'planned'
export const fillLabel: Record<Fill, string> = { earned: '修得', registered: '今期の登録', planned: '計画' }
export const fillOrder: Fill[] = ['earned', 'registered', 'planned']

export type Segment = { key: string; color: string; fill: Fill; value: number; label: string }

const fillStyle = (color: string): CSSProperties => ({ ['--c' as string]: color })

/** 修得 → 今期 → 計画、同じ塗り方の中は区分の色の順に並べる（呼び出し側は色の順で渡す）。 */
export function sortSegments(segments: Segment[]): Segment[] {
  return [...segments].filter((s) => s.value > 0).sort((a, b) => fillOrder.indexOf(a.fill) - fillOrder.indexOf(b.fill))
}

/** 横向きの積み上げ棒。max がトラックの長さ（必須単位など）。 */
export function StackedBar({ segments, max, className, label }: { segments: Segment[]; max: number; className?: string; label: string }) {
  const shown = sortSegments(segments)
  const total = shown.reduce((a, s) => a + s.value, 0)
  return (
    <div className={cn('relative h-2.5 overflow-hidden rounded-[4px] bg-track', className)} role="img" aria-label={label}>
      <div className="flex h-full overflow-hidden rounded-r-[4px]" style={{ width: `${Math.min(100, (total / Math.max(max, 1)) * 100)}%` }}>
        {shown.map((s, i) => (
          <Tooltip key={s.key + s.fill}>
            <TooltipTrigger asChild>
              <div
                className={cn(`fill-${s.fill}`, i < shown.length - 1 && 'border-r-2 border-card')}
                style={{ ...fillStyle(s.color), flexGrow: s.value, flexBasis: 0 }}
              />
            </TooltipTrigger>
            <TooltipContent>{s.label}: {fillLabel[s.fill]} {credits(s.value)} 単位</TooltipContent>
          </Tooltip>
        ))}
      </div>
    </div>
  )
}

/** 塗り方の凡例（色はグレーで、塗り方だけを見せる）。 */
export function FillLegend({ fills = fillOrder, values }: { fills?: Fill[]; values?: Partial<Record<Fill, number>> }) {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {fills.map((f) => (
        <li key={f} className="flex items-center gap-1.5">
          <span className={cn('size-3 rounded-[3px]', `fill-${f}`)} style={fillStyle('var(--muted-foreground)')} aria-hidden />
          {fillLabel[f]}
          {values?.[f] != null && <span className="font-medium text-foreground tabular-nums">{credits(values[f])}</span>}
        </li>
      ))}
    </ul>
  )
}

/** 区分の色の凡例。 */
export function CategoryLegend({ items }: { items: { name: string; color: string; value?: number }[] }) {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1.5 text-xs text-muted-foreground">
      {items.map((it) => (
        <li key={it.name} className="flex items-center gap-1.5">
          <span className="size-2.5 rounded-[3px] fill-earned" style={fillStyle(it.color)} aria-hidden />
          <span className="text-foreground">{it.name}</span>
          {it.value != null && <span className="tabular-nums">{credits(it.value)}</span>}
        </li>
      ))}
    </ul>
  )
}

export type Column = {
  key: string
  label: string // 軸の下の短い名前（例: 前）
  group: string // まとまり（例: 1年）
  segments: Segment[]
  title: string // ツールチップの見出し
  dim?: boolean
  marker?: boolean // 今期など
}

/** 縦向きの積み上げ棒を並べたグラフ（学期ごとの単位）。limit があれば履修上限の点線を引く。 */
export function ColumnChart({ columns, limit, onSelect, height = 140 }: { columns: Column[]; limit?: number; onSelect?: (c: Column) => void; height?: number }) {
  const sum = (c: Column) => c.segments.reduce((a, s) => a + s.value, 0)
  const top = Math.max(limit ?? 0, ...columns.map(sum), 1) * 1.12
  const y = (v: number) => (v / top) * height
  const groups: { group: string; cols: Column[] }[] = []
  for (const c of columns) {
    const g = groups.at(-1)
    if (g && g.group === c.group) g.cols.push(c)
    else groups.push({ group: c.group, cols: [c] })
  }
  return (
    <div>
      <div className="relative" style={{ height }}>
        <div className="absolute inset-x-0 bottom-0 h-px bg-border" />
        {limit != null && limit > 0 && (
          <div className="absolute inset-x-0 border-t border-dashed border-muted-foreground/50" style={{ bottom: y(limit) }}>
            <span className="absolute -top-4 right-0 text-[10px] text-muted-foreground">上限 {credits(limit)}</span>
          </div>
        )}
        <div className="absolute inset-0 flex items-end gap-2 sm:gap-3">
          {groups.map((g) => (
            <div key={g.group} className="flex h-full flex-1 items-end gap-1">
              {g.cols.map((c) => {
                const shown = sortSegments(c.segments)
                const total = sum(c)
                return (
                  <Tooltip key={c.key}>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        onClick={() => onSelect?.(c)}
                        className={cn('group relative flex h-full flex-1 flex-col justify-end rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring', c.dim && 'opacity-40')}
                        aria-label={`${c.title}: ${credits(total)} 単位`}
                      >
                        <span className="absolute inset-0 rounded-sm group-hover:bg-muted/60" />
                        {total > 0 && <span className="relative mb-0.5 text-center text-[10px] tabular-nums text-muted-foreground">{credits(total)}</span>}
                        <span className="relative flex flex-col-reverse overflow-hidden rounded-t-[4px]" style={{ height: y(total) }}>
                          {shown.map((s, i) => (
                            <span
                              key={s.key + s.fill}
                              className={cn(`fill-${s.fill}`, i < shown.length - 1 && 'border-t-2 border-card')}
                              style={{ ...fillStyle(s.color), flexGrow: s.value, flexBasis: 0 }}
                            />
                          ))}
                        </span>
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <ColumnTooltip title={c.title} segments={shown} />
                    </TooltipContent>
                  </Tooltip>
                )
              })}
            </div>
          ))}
        </div>
      </div>
      <div className="mt-1.5 flex gap-2 sm:gap-3">
        {groups.map((g) => (
          <div key={g.group} className="flex-1">
            <div className="flex gap-1">
              {g.cols.map((c) => (
                <span key={c.key} className={cn('flex-1 text-center text-[10px] text-muted-foreground', c.marker && 'font-semibold text-foreground')}>{c.label}</span>
              ))}
            </div>
            <div className="mt-0.5 border-t pt-0.5 text-center text-[11px] text-muted-foreground">{g.group}</div>
          </div>
        ))}
      </div>
    </div>
  )
}

function ColumnTooltip({ title, segments }: { title: string; segments: Segment[] }): ReactNode {
  return (
    <div className="space-y-1">
      <p className="font-medium">{title}</p>
      {segments.length === 0 && <p>0 単位</p>}
      {segments.map((s) => (
        <p key={s.key + s.fill} className="flex items-center gap-1.5">
          <span className={cn('size-2.5 rounded-[2px]', `fill-${s.fill}`)} style={fillStyle(s.color)} />
          {s.label}（{fillLabel[s.fill]}）<span className="ml-auto pl-2 tabular-nums">{credits(s.value)}</span>
        </p>
      ))}
    </div>
  )
}
