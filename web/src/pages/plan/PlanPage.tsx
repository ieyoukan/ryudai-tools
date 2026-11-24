import { AlertTriangle, CircleCheck, RefreshCw, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import type { GraduationStatus, PlanYear } from '@/api/gen/academic'
import { useDeletePlan, useGraduation } from '@/api/hooks'
import { CategoryLegend, ColumnChart, FillLegend, StackedBar, type Column } from '@/components/charts'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger,
} from '@/components/ui/dialog'
import { OTHER, categoryColors, type CategoryColors } from '@/lib/categories'
import { termSegments, totalSegments } from '@/lib/credits'
import { credits } from '@/lib/format'
import { cn } from '@/lib/utils'
import { CategoryOutlook } from './CategoryOutlook'
import { PlanLoading } from './PlanLoading'
import { TermCard } from './TermCard'

export function PlanPage() {
  const { data: st, error, isPending, refetch, isFetching } = useGraduation()

  return (
    <div className="space-y-4 md:space-y-6">
      <div className="flex items-center justify-between gap-3">
        <h1 className="hidden text-xl font-semibold md:block">卒業までの計画</h1>
        {st && (
          <Button variant="ghost" size="sm" className="ml-auto text-muted-foreground" onClick={() => refetch()} disabled={isFetching}>
            <RefreshCw className={cn(isFetching && 'animate-spin')} />
            最新にする
          </Button>
        )}
      </div>

      {isPending && <PlanLoading />}
      {error && (
        <Card>
          <CardContent className="flex flex-col items-start gap-3">
            <p className="flex items-center gap-2 font-medium"><AlertTriangle className="size-4 text-critical" />読み込めませんでした</p>
            <p className="text-sm text-muted-foreground">{error.message}</p>
            <Button variant="outline" size="sm" onClick={() => refetch()}>もう一度読み込む</Button>
          </CardContent>
        </Card>
      )}
      {st?.plan && <Plan status={st} />}

      <p className="text-center text-xs text-muted-foreground">
        非公式のツールです。卒業の判定は大学の成績表・便覧で確認してください（卒業要件の判定は<a href="/grades" className="underline underline-offset-4">成績</a>）。
      </p>
    </div>
  )
}

function Plan({ status: st }: { status: GraduationStatus }) {
  const plan = st.plan!
  const years = plan.years
  const current = years.find((y) => y.current)?.level ?? years[0]?.level ?? 1
  const [level, setLevel] = useState(() => {
    const m = /^#year-(\d+)$/.exec(location.hash)
    return m && years.some((y) => y.level === Number(m[1])) ? Number(m[1]) : current
  })
  useEffect(() => history.replaceState(null, '', '#year-' + level), [level])
  const year = years.find((y) => y.level === level) ?? years[0]
  const colors = useMemo(() => categoryColors(st), [st])

  return (
    <>
      <Summary status={st} colors={colors} />
      {plan.deviations.length > 0 && (
        <Card size="sm">
          <CardContent className="space-y-1.5 text-sm">
            <p className="flex items-center gap-2 font-medium"><AlertTriangle className="size-4 text-warning" />計画どおりに進んでいない科目</p>
            <ul className="space-y-1 pl-6 text-muted-foreground">
              {plan.deviations.map((d) => (
                <li key={d.item.id}><span className="text-foreground">{d.item.title}</span>（{d.item.year_term}）: {d.reason}</li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
          <CardTitle>学期ごとの単位</CardTitle>
          <FillLegend />
        </CardHeader>
        <CardContent className="space-y-4">
          <ColumnChart columns={termColumns(st, years, level, colors)} limit={plan.credit_limit || undefined} onSelect={(c) => setLevel(Number(c.key.split(':')[0]))} />
          <YearSwitch years={years} level={level} onChange={setLevel} />
        </CardContent>
      </Card>

      {year && (
        <div className="grid items-start gap-4 md:grid-cols-2">
          {year.terms.map((t) => <TermCard key={t.year_term} term={t} status={st} colors={colors} />)}
        </div>
      )}

      <CategoryOutlook status={st} colors={colors} />
      {plan.terms.length > 0 && <DeleteAll />}
    </>
  )
}

/** 卒業に必要な単位のうち、修得・今期の登録・計画でどこまで埋まるか（区分ごとに色分け）。 */
function Summary({ status: st, colors }: { status: GraduationStatus; colors: CategoryColors }) {
  const plan = st.plan!
  const segments = totalSegments(st, colors)
  const sum = (f: 'earned' | 'registered' | 'planned') => segments.filter((s) => s.fill === f).reduce((a, s) => a + s.value, 0)
  const rest = plan.total_shortfall_after_plan
  const byCategory = new Map<string, number>()
  for (const s of segments) byCategory.set(s.key, (byCategory.get(s.key) ?? 0) + s.value)
  return (
    <Card>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-2">
          <div>
            <p className="text-xs text-muted-foreground">卒業に必要な単位</p>
            <p className="mt-0.5 text-3xl font-semibold tracking-tight">
              {credits(st.total_earned)}<span className="ml-1 text-base font-normal text-muted-foreground">/ {credits(st.total_required)}</span>
            </p>
          </div>
          {rest > 0 ? (
            <p className="flex items-center gap-1.5 text-sm"><AlertTriangle className="size-4 text-warning" />計画どおりでも<strong className="tabular-nums">あと {credits(rest)} 単位</strong></p>
          ) : (
            <p className="flex items-center gap-1.5 text-sm"><CircleCheck className="size-4 text-good" />計画どおりなら必要単位に届きます</p>
          )}
        </div>
        <StackedBar
          className="h-4"
          max={st.total_required}
          segments={segments}
          label={`卒業に必要な ${credits(st.total_required)} 単位のうち、修得 ${credits(sum('earned'))}、今期の登録 ${credits(sum('registered'))}、計画 ${credits(sum('planned'))}、不足 ${credits(rest)}`}
        />
        <FillLegend values={{ earned: sum('earned'), registered: sum('registered'), planned: sum('planned') }} />
        <CategoryLegend items={[...colors.order, OTHER].filter((n, i, a) => a.indexOf(n) === i && byCategory.has(n)).map((n) => ({ name: n, color: colors.color(n === OTHER ? undefined : n), value: byCategory.get(n) }))} />
      </CardContent>
    </Card>
  )
}

function termColumns(st: GraduationStatus, years: PlanYear[], level: number, colors: CategoryColors): Column[] {
  return years.flatMap((y) =>
    y.terms.map((t) => ({
      key: `${y.level}:${t.year_term}`,
      label: t.label.replace('期', ''),
      group: `${y.level}年`,
      segments: termSegments(st, t, colors),
      title: `${t.year_term}${t.state === 'current' ? '（今期）' : ''}`,
      dim: y.level !== level,
      marker: t.state === 'current',
    })),
  )
}

/** 年次の切り替え（グラフの棒を押しても切り替わる）。 */
function YearSwitch({ years, level, onChange }: { years: PlanYear[]; level: number; onChange: (l: number) => void }) {
  return (
    <div className="flex gap-1 rounded-lg bg-muted p-1" role="tablist" aria-label="年次">
      {years.map((y) => (
        <button
          key={y.level}
          type="button"
          role="tab"
          aria-selected={y.level === level}
          onClick={() => onChange(y.level)}
          className={cn(
            'flex h-10 flex-1 items-center justify-center gap-1.5 rounded-md text-sm text-muted-foreground transition-colors',
            y.level === level && 'bg-card font-medium text-foreground shadow-xs',
          )}
        >
          {y.level}年
          {y.current && <span className="size-1.5 rounded-full bg-primary" aria-label="今の年次" />}
        </button>
      ))}
    </div>
  )
}

function DeleteAll() {
  const del = useDeletePlan()
  const [open, setOpen] = useState(false)
  return (
    <div className="flex justify-center md:justify-end">
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogTrigger asChild>
          <Button variant="ghost" size="sm" className="text-muted-foreground"><Trash2 />計画をすべて削除</Button>
        </DialogTrigger>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>計画をすべて削除しますか？</DialogTitle>
            <DialogDescription>来期以降に入れた科目と、自分で選んだ今期の科目の区分を消します。成績と履修登録はそのままです。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild><Button variant="outline">やめる</Button></DialogClose>
            <Button variant="destructive" disabled={del.isPending} onClick={() => del.mutate(undefined, { onSuccess: () => setOpen(false) })}>削除する</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
