import { CircleCheck, CircleDashed, CircleHelp, CircleX, Clock } from 'lucide-react'

import type { RequiredCheck } from '@/api/gen/academic'
import { SectionTitle } from '@/components/page'
import { Card, CardContent } from '@/components/ui/card'
import { credits } from '@/lib/format'
import { cn } from '@/lib/utils'

const states = {
  passed: { label: '修得済み', icon: CircleCheck, tone: 'text-good', bar: 'bg-good' },
  enrolled: { label: '履修中', icon: Clock, tone: 'text-primary', bar: 'bg-primary' },
  planned: { label: '追加予定', icon: CircleDashed, tone: 'text-primary', bar: 'bg-primary/50' },
  not_passed: { label: '未修得', icon: CircleX, tone: 'text-critical', bar: 'bg-critical' },
  unverified: { label: '確認不能', icon: CircleHelp, tone: 'text-muted-foreground', bar: 'bg-muted-foreground/50' },
} as const
type State = keyof typeof states

/** 便覧の必修科目を、状態ごとの件数の棒と一覧で。 */
export function Required({ r }: { r?: RequiredCheck }) {
  if (!r) return null
  const total = r.required.length
  const order: State[] = ['passed', 'enrolled', 'planned', 'not_passed', 'unverified']
  return (
    <Card>
      <CardContent className="space-y-3">
        <SectionTitle>必修科目</SectionTitle>
        {r.catalog && <p className="text-xs text-muted-foreground">{r.catalog.enter_year}年度便覧・{r.catalog.faculty} {r.catalog.course}（{r.catalog.scope}）</p>}
        {total > 0 && (
          <>
            <div className="flex h-3 overflow-hidden rounded-[4px] bg-track" role="img" aria-label={order.map((s) => `${states[s].label} ${r.counts[s] ?? 0}`).join('、')}>
              {order.map((s) => (r.counts[s] ?? 0) > 0 && (
                <div key={s} className={cn(states[s].bar, 'border-r-2 border-card last:border-0')} style={{ flexGrow: r.counts[s], flexBasis: 0 }} />
              ))}
            </div>
            <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
              {order.map((s) => (r.counts[s] ?? 0) > 0 && (
                <li key={s} className="flex items-center gap-1"><span className={cn('size-2.5 rounded-[3px]', states[s].bar)} />{states[s].label} <span className="font-medium text-foreground tabular-nums">{r.counts[s]}</span></li>
              ))}
            </ul>
            <ul className="divide-y rounded-lg border">
              {[...r.required].sort((a, b) => order.indexOf(b.status as State) - order.indexOf(a.status as State)).map((s, i) => {
                const st = states[s.status as State] ?? states.unverified
                return (
                  <li key={i} className="flex items-start gap-2 px-3 py-2">
                    <st.icon className={cn('mt-0.5 size-4 shrink-0', st.tone)} aria-hidden />
                    <div className="min-w-0 flex-1">
                      <p className="text-sm"><span className={cn('mr-2 whitespace-nowrap text-xs font-medium', st.tone)}>{st.label}</span>{s.title} <span className="text-xs text-muted-foreground">{s.numbering_code} ・ {credits(s.credits)}単位</span></p>
                      {s.evidence.map((e, j) => (
                        <p key={j} className="text-xs text-muted-foreground">{[e.year && `${e.year}年度`, e.title, e.grade && `評価 ${e.grade}`, e.note].filter(Boolean).join(' ・ ')}</p>
                      ))}
                    </div>
                    <a href={`${s.source.url}#page=${s.page}`} target="_blank" rel="noopener" className="shrink-0 text-xs text-muted-foreground underline">便覧</a>
                  </li>
                )
              })}
            </ul>
          </>
        )}
        {r.selective_required.length > 0 && (
          <details className="text-sm">
            <summary className="cursor-pointer text-muted-foreground">選択必修の候補（組合せ・必要単位は便覧で確認）</summary>
            <ul className="mt-2 space-y-1 text-xs text-muted-foreground">
              {r.selective_required.map((s, i) => <li key={i}>{s.title}（{s.numbering_code}、{credits(s.credits)}単位）: {(states[s.status as State] ?? states.unverified).label}</li>)}
            </ul>
          </details>
        )}
        {r.notes.map((n, i) => <p key={i} className="text-xs text-muted-foreground">{n}</p>)}
      </CardContent>
    </Card>
  )
}
