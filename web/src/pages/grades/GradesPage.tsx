import { AlertTriangle, CircleCheck, RefreshCw, XCircle } from 'lucide-react'
import { useMemo, useState } from 'react'

import type { GraduationStatus } from '@/api/gen/academic'
import type { Grade } from '@/api/gen/kyomu'
import { useGraduationWithGrades, useSaveLectureCategories } from '@/api/hooks'
import { CategoryLegend, FillLegend, StackedBar } from '@/components/charts'
import { ErrorState, PageHeader, SectionTitle, Stat } from '@/components/page'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { OTHER, categoryColors, requirementOf, type CategoryColors } from '@/lib/categories'
import { totalSegments } from '@/lib/credits'
import { credits } from '@/lib/format'
import { cn } from '@/lib/utils'
import { PlanLoading } from '../plan/PlanLoading'
import { Required } from './Required'
import { Thesis } from './Thesis'
import { Verdict } from './Verdict'

/** 成績と卒業要件。卒業要件を満たせるかの判定はこのページにだけ出す。 */
export function GradesPage() {
  const { data: st, error, isPending, refetch, isFetching } = useGraduationWithGrades()
  const colors = useMemo(() => (st ? categoryColors(st) : null), [st])
  return (
    <div className="space-y-4 md:space-y-6">
      <PageHeader
        title="成績・卒業要件"
        actions={st && <Button variant="ghost" size="sm" className="text-muted-foreground" onClick={() => refetch()} disabled={isFetching}><RefreshCw className={cn(isFetching && 'animate-spin')} />最新にする</Button>}
      />
      {isPending && <PlanLoading />}
      {error && <ErrorState message={error.message} onRetry={() => refetch()} />}
      {st && colors && (
        <>
          <Verdict status={st} />
          <Overview st={st} colors={colors} />
          <Unmapped st={st} />
          <Thesis t={st.thesis} />
          <CategoryReach st={st} colors={colors} />
          <GradeDistribution st={st} />
          {(st.failed_courses ?? []).length > 0 && <Failed st={st} />}
          <Fusion st={st} />
          <Required r={st.required_courses} />
          <GradeList st={st} colors={colors} />
          <RequirementTable st={st} />
          {st.notes.length > 0 && (
            <details className="rounded-xl border bg-card px-4 py-3 text-sm">
              <summary className="cursor-pointer font-medium">補足（{st.notes.length} 件）</summary>
              <div className="mt-2 space-y-1 text-xs text-muted-foreground">{st.notes.map((n, i) => <p key={i}>{n}</p>)}</div>
            </details>
          )}
        </>
      )}
      <p className="text-center text-xs text-muted-foreground">非公式のツールのため、表示は参考情報です。卒業・履修の判断は大学の公式の成績表・便覧で確認してください。</p>
    </div>
  )
}

function Overview({ st, colors }: { st: GraduationStatus; colors: CategoryColors }) {
  const segments = totalSegments(st, colors).filter((s) => s.fill !== 'planned')
  const registered = st.this_term?.registered_credits ?? 0
  const byCat = new Map<string, number>()
  for (const s of segments) byCat.set(s.key, (byCat.get(s.key) ?? 0) + s.value)
  return (
    <Card>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <Stat label="修得 / 卒業に必要" value={credits(st.total_earned)} unit={`/ ${credits(st.total_required)} 単位`} />
          <Stat label="卒業まであと" value={credits(st.total_shortfall)} unit="単位" />
          <Stat label={`今期の登録${st.this_term ? `（${st.this_term.year_term}）` : ''}`} value={credits(registered)} unit="単位" />
          <Stat label="GPA" value={st.gpa != null ? credits(st.gpa) : '―'} hint={st.per_term_after_this != null ? `今期の後 1 学期あたり ${credits(st.per_term_after_this)} 単位` : undefined} />
        </div>
        <StackedBar className="h-4" max={st.total_required} segments={segments} label={`卒業に必要な ${credits(st.total_required)} 単位のうち、修得 ${credits(st.total_earned)}、今期の登録 ${credits(registered)}`} />
        <FillLegend fills={['earned', 'registered']} />
        <CategoryLegend items={[...colors.order, OTHER].filter((n, i, a) => a.indexOf(n) === i && byCat.has(n)).map((n) => ({ name: n, color: colors.color(n === OTHER ? undefined : n), value: byCat.get(n) }))} />
      </CardContent>
    </Card>
  )
}

/** 区分ごとの到達度。今期の登録がすべて単位になっても足りない区分を上に。 */
function CategoryReach({ st, colors }: { st: GraduationStatus; colors: CategoryColors }) {
  const rows = (st.plan?.categories ?? [])
    .map((c) => ({ ...c, after: Math.max(0, c.required - c.earned - c.registered) }))
    .sort((a, b) => b.after - a.after || (b.required - b.earned) - (a.required - a.earned))
  if (rows.length === 0) return null
  const unmet = rows.filter((r) => r.after > 0).length
  return (
    <Card>
      <CardContent className="space-y-3">
        <SectionTitle actions={<FillLegend fills={['earned', 'registered']} />}>区分ごとの到達度</SectionTitle>
        <p className="text-xs text-muted-foreground">{unmet > 0 ? `今期の登録がすべて単位になっても、${unmet} 区分が足りません` : 'すべての区分が、今期の登録で必須単位に届く見込みです'}（棒の右端が必須単位）</p>
        <ul className="space-y-3">
          {rows.map((c) => (
            <li key={c.section + c.name} className="space-y-1.5">
              <div className="flex items-baseline justify-between gap-2 text-sm">
                <span className="flex min-w-0 items-center gap-1.5 truncate">
                  <span className="size-2.5 shrink-0 rounded-[3px] fill-earned" style={{ ['--c' as string]: colors.color(c.name) }} aria-hidden />
                  {c.name}<span className="text-xs text-muted-foreground">{c.section}</span>
                </span>
                <span className="flex shrink-0 items-center gap-2 text-xs">
                  <span className="tabular-nums text-muted-foreground">{credits(c.earned)}{c.registered > 0 && `+${credits(c.registered)}`}/{credits(c.required)}</span>
                  {c.after > 0 ? (
                    <span className="flex items-center gap-1 font-medium"><AlertTriangle className="size-3.5 text-warning" />あと {credits(c.after)}</span>
                  ) : (
                    <span className="flex items-center gap-1 text-muted-foreground"><CircleCheck className="size-3.5 text-good" />{c.earned >= c.required ? '達成' : '今期で達成'}</span>
                  )}
                </span>
              </div>
              <StackedBar
                max={c.required}
                segments={(['earned', 'registered'] as const).map((f) => ({ key: c.name, color: colors.color(c.name), fill: f, value: c[f], label: c.name }))}
                label={`${c.name}: 必須 ${credits(c.required)}、修得 ${credits(c.earned)}、今期 ${credits(c.registered)}`}
              />
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

const gradeOrder = ['S', 'A', 'B', 'C', 'D', 'P', 'R', 'N', 'F']

/** 評価の分布（単一の系列なので色は 1 つ）。 */
function GradeDistribution({ st }: { st: GraduationStatus }) {
  const entries = Object.entries(st.grade_counts ?? {}).sort((a, b) => {
    const ia = gradeOrder.indexOf(a[0]), ib = gradeOrder.indexOf(b[0])
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib)
  })
  if (entries.length === 0) return null
  const max = Math.max(...entries.map(([, n]) => n))
  return (
    <Card>
      <CardContent className="space-y-3">
        <SectionTitle>評価の分布</SectionTitle>
        <ul className="space-y-1.5">
          {entries.map(([g, n]) => (
            <li key={g} className="grid grid-cols-[2rem_1fr_3rem] items-center gap-2 text-sm">
              <span className={cn('font-medium', g === 'F' && 'text-critical')}>{g || '―'}</span>
              <span className="h-3 overflow-hidden rounded-r-[4px]">
                <span className={cn('block h-full rounded-r-[4px]', g === 'F' ? 'bg-critical' : 'bg-primary')} style={{ width: `${(n / max) * 100}%` }} />
              </span>
              <span className="text-right text-xs tabular-nums text-muted-foreground">{n} 件</span>
            </li>
          ))}
        </ul>
        {st.remote_credits > 0 && <p className="text-xs text-muted-foreground">遠隔授業 {credits(st.remote_credits)} 単位（卒業単位に数えるのは最大 60 単位まで）</p>}
      </CardContent>
    </Card>
  )
}

function Failed({ st }: { st: GraduationStatus }) {
  return (
    <Card>
      <CardContent className="space-y-2">
        <SectionTitle>不合格のまま修得していない科目</SectionTitle>
        <ul className="divide-y rounded-lg border">
          {st.failed_courses!.map((f, i) => (
            <li key={i} className="flex items-center gap-2 px-3 py-2 text-sm">
              <XCircle className="size-4 shrink-0 text-critical" />
              <div className="min-w-0 flex-1">
                <p className="truncate">{f.title}</p>
                <p className="text-xs text-muted-foreground">
                  {f.year} {f.term} ・ {f.category} ・ {credits(f.credits)}単位
                  {f.counts_toward_requirement === true && ' ・ 必須単位のある区分'}
                  {f.counts_toward_requirement === false && ' ・ 卒業要件の必須区分外'}
                </p>
              </div>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

function Fusion({ st }: { st: GraduationStatus }) {
  const c = st.curriculum
  if (!c || (!c.fusion_rule && !(c.fusion_groups ?? []).length)) return null
  return (
    <Card>
      <CardContent className="space-y-2">
        <SectionTitle>工学融合のコース別の見通し</SectionTitle>
        {c.fusion_rule && (
          <p className="text-xs text-muted-foreground">
            同一コースから {credits(c.fusion_rule.minimum_credits)} 単位以上（<a href={`${c.fusion_rule.evidence.source.url}#page=${c.fusion_rule.evidence.page}`} target="_blank" rel="noopener" className="underline">便覧の根拠</a>）
          </p>
        )}
        <ul className="space-y-2">
          {c.fusion_groups.map((g) => (
            <li key={g.course} className="space-y-1">
              <div className="flex justify-between text-sm"><span>{g.course}</span><span className="text-xs text-muted-foreground">修得 {credits(g.earned_credits)} ＋ 今期 {credits(g.registered_credits)} ・ 不足 {credits(g.shortfall_after_plan)}</span></div>
              {c.fusion_rule && (
                <div className="flex h-2 overflow-hidden rounded-[4px] bg-track">
                  <div className="bg-primary" style={{ width: `${Math.min(100, (g.earned_credits / c.fusion_rule.minimum_credits) * 100)}%` }} />
                  <div className="border-l-2 border-card bg-primary/45" style={{ width: `${Math.min(100, (g.registered_credits / c.fusion_rule.minimum_credits) * 100)}%` }} />
                </div>
              )}
            </li>
          ))}
        </ul>
        {c.notes.map((n, i) => <p key={i} className="text-xs text-muted-foreground">{n}</p>)}
        {(c.unclassified ?? []).map((n, i) => <p key={i} className="text-xs text-muted-foreground">要確認: {n}</p>)}
      </CardContent>
    </Card>
  )
}

/** 区分が分からない今期の登録科目（分かっている科目は選び直させない）。 */
function Unmapped({ st }: { st: GraduationStatus }) {
  const save = useSaveLectureCategories()
  const list = (st.this_term?.unmapped ?? []).filter((u) => u.lecture_code)
  const [picked, setPicked] = useState<Record<string, string>>({})
  if (list.length === 0) return null
  const items = st.requirements.filter((r) => r.kind === 'item')
  const choice = (code: string, suggested?: string) => picked[code] ?? suggested ?? ''
  return (
    <Card className="border-warning/50">
      <CardContent className="space-y-3">
        <SectionTitle>区分が分からない今期の科目</SectionTitle>
        <p className="flex items-start gap-2 text-xs text-muted-foreground"><AlertTriangle className="size-4 shrink-0 text-warning" />{list.length} 件をグラフと見通しに含めていません。区分を選んで保存してください（候補は講義コードが近い、成績のある科目の区分です）。</p>
        <ul className="space-y-2">
          {list.map((u) => (
            <li key={u.lecture_code} className="flex flex-wrap items-center gap-2">
              <span className="min-w-0 flex-1 text-sm">{u.title} <span className="text-xs text-muted-foreground">{credits(u.credits)}単位</span></span>
              <Select value={choice(u.lecture_code!, u.suggested)} onValueChange={(v) => setPicked((p) => ({ ...p, [u.lecture_code!]: v }))}>
                <SelectTrigger className="h-10 w-full sm:w-64 md:h-8" aria-label={`${u.title} の区分`}><SelectValue placeholder="区分を選ぶ" /></SelectTrigger>
                <SelectContent>{items.map((r) => <SelectItem key={r.section + r.name} value={r.name}>{r.name}（{r.section}）</SelectItem>)}</SelectContent>
              </Select>
            </li>
          ))}
        </ul>
        <Button
          size="sm"
          disabled={save.isPending || list.every((u) => !choice(u.lecture_code!, u.suggested))}
          onClick={() => save.mutate(Object.fromEntries(list.filter((u) => choice(u.lecture_code!, u.suggested)).map((u) => [u.lecture_code!, choice(u.lecture_code!, u.suggested)])))}
        >
          区分を保存
        </Button>
      </CardContent>
    </Card>
  )
}

/** 成績一覧（年度・学期ごと）。 */
function GradeList({ st, colors }: { st: GraduationStatus; colors: CategoryColors }) {
  const [open, setOpen] = useState(false)
  const grades = useMemo(() => st.grades ?? [], [st.grades])
  const groups = useMemo(() => {
    const m = new Map<string, Grade[]>()
    for (const g of [...grades].sort((a, b) => b.year - a.year || b.term.localeCompare(a.term))) {
      const k = `${g.year} ${g.term}`
      m.set(k, [...(m.get(k) ?? []), g])
    }
    return [...m.entries()]
  }, [grades])
  if (grades.length === 0) return null
  return (
    <Card>
      <CardContent className="space-y-3">
        <SectionTitle actions={<Button variant="ghost" size="sm" onClick={() => setOpen((v) => !v)}>{open ? '閉じる' : `すべて表示（${grades.length} 件）`}</Button>}>成績一覧</SectionTitle>
        {open && groups.map(([k, list]) => (
          <section key={k} className="space-y-1">
            <h3 className="text-xs font-medium text-muted-foreground">{k} ・ 修得 {credits(list.filter((g) => g.passed).reduce((a, g) => a + g.credits, 0))} 単位</h3>
            <ul className="divide-y rounded-lg border">
              {list.map((g, i) => {
                const cat = requirementOf(st, g.category)
                return (
                  <li key={i} className="flex min-h-11 items-center gap-2 px-3 py-1.5">
                    <span className="size-2 shrink-0 rounded-[2px] fill-earned" style={{ ['--c' as string]: colors.color(cat) }} aria-hidden />
                    <div className="min-w-0 flex-1">
                      <p className={cn('truncate text-sm', !g.passed && 'text-critical')}>{g.title}{g.remote && <span className="ml-1 text-xs text-muted-foreground">遠隔</span>}</p>
                      <p className="truncate text-xs text-muted-foreground">{cat ?? g.category}</p>
                    </div>
                    <span className="text-xs tabular-nums text-muted-foreground">{credits(g.credits)}</span>
                    <span className={cn('w-6 text-right text-sm font-medium', !g.passed && 'text-critical')}>{g.grade}</span>
                  </li>
                )
              })}
            </ul>
          </section>
        ))}
      </CardContent>
    </Card>
  )
}

/** 個別成績表の集計表そのまま。 */
function RequirementTable({ st }: { st: GraduationStatus }) {
  return (
    <details className="rounded-xl border bg-card px-4 py-3">
      <summary className="cursor-pointer text-sm font-medium">個別成績表の区分ごとの単位（全行）</summary>
      <div className="mt-3 overflow-x-auto">
        <table className="w-full text-sm">
          <thead><tr className="border-b text-xs text-muted-foreground"><th className="py-1.5 text-left font-normal">区分</th><th className="font-normal">必須</th><th className="font-normal">修得</th><th className="font-normal">不足</th></tr></thead>
          <tbody>
            {st.requirements.map((r, i) => (
              <tr key={i} className={cn('border-b last:border-0', r.kind !== 'item' && 'bg-muted/40 font-medium')}>
                <td className="py-1.5">{r.name} <span className="text-xs font-normal text-muted-foreground">{r.section}</span></td>
                <td className="text-center tabular-nums">{r.required != null ? credits(r.required) : '―'}</td>
                <td className="text-center tabular-nums">{r.earned != null ? credits(r.earned) : '―'}</td>
                <td className={cn('text-center tabular-nums', (r.shortfall ?? 0) > 0 && 'text-critical')}>{r.shortfall != null ? credits(r.shortfall) : '―'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </details>
  )
}
