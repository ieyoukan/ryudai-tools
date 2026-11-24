import { AlertTriangle, Plus, Sparkles, X, XCircle } from 'lucide-react'
import { useState, type ReactNode } from 'react'

import type { GraduationStatus, PlanYearTerm, TakenCourse } from '@/api/gen/academic'
import { useDeletePlanItem, useSaveLectureCategories } from '@/api/hooks'
import { StackedBar } from '@/components/charts'
import { termSegments } from '@/lib/credits'
import type { CategoryColors } from '@/lib/categories'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { credits } from '@/lib/format'
import { cn } from '@/lib/utils'
import { AddItemDialog } from './AddItemDialog'
import { Suggestions } from './Suggestions'

export function TermCard({ term, status, colors }: { term: PlanYearTerm; status: GraduationStatus; colors: CategoryColors }) {
  const [showSuggest, setShowSuggest] = useState(false)
  const plannable = term.state === 'current' || term.state === 'future'
  const limit = status.plan?.credit_limit ?? 0
  const used = term.registered_credits + term.planned_credits
  const total = term.state === 'past' ? term.earned_credits : term.earned_credits + used

  return (
    <Card id={'term-' + term.year_term} className={cn('gap-3', term.state === 'current' && 'ring-2 ring-primary', term.state === 'after' && 'opacity-60')}>
      <CardHeader className="flex flex-row items-baseline justify-between gap-2">
        <CardTitle className="flex items-baseline gap-2 text-base">
          {term.label}
          <span className="text-xs font-normal text-muted-foreground">{term.year_term.slice(0, 4)}年度</span>
          {term.state === 'current' && <Badge className="self-center">今期</Badge>}
          {term.state === 'after' && <Badge variant="outline" className="self-center">卒業予定より後</Badge>}
        </CardTitle>
        <p className="text-xl font-semibold tabular-nums">
          {credits(total)}<span className="ml-0.5 text-xs font-normal text-muted-foreground">単位</span>
        </p>
      </CardHeader>

      <CardContent className="space-y-3">
        {plannable && limit > 0 && (
          <div className="space-y-1.5">
            <StackedBar max={limit} segments={termSegments(status, term, colors)} label={`履修上限 ${credits(limit)} 単位のうち ${credits(used)} 単位`} />
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs text-muted-foreground">履修上限 {credits(limit)}</span>
              <span className={cn('text-xs tabular-nums text-muted-foreground', used > limit && 'flex items-center gap-1 font-medium text-critical')}>
                {used > limit && <AlertTriangle className="size-3.5" />}
                {used > limit ? `上限を ${credits(used - limit)} 超過` : `上限まであと ${credits(limit - used)}`}
              </span>
            </div>
          </div>
        )}

        {term.state === 'current' ? (
          <CurrentLectures courses={term.taken} status={status} />
        ) : term.taken.length > 0 ? (
          <Rows>
            {term.taken.map((c, i) => (
              <Row key={i} title={c.title} sub={c.category && shortCategory(c.category)}>
                <span className="text-xs tabular-nums text-muted-foreground">{credits(c.credits)}</span>
                <GradeMark course={c} />
              </Row>
            ))}
          </Rows>
        ) : term.state === 'past' ? (
          <Empty>この学期の成績はありません</Empty>
        ) : null}

        {term.items.length > 0 && <PlanItems term={term} />}
        {term.state === 'future' && term.items.length === 0 && <Empty>まだ科目がありません</Empty>}

        {plannable && (
          <div className="space-y-3 pt-1">
            <div className="grid grid-cols-2 gap-2">
              <Button variant={showSuggest ? 'secondary' : 'default'} className="h-10 md:h-9" onClick={() => setShowSuggest((v) => !v)} aria-expanded={showSuggest}>
                <Sparkles />おすすめ
              </Button>
              <AddItemDialog term={term.year_term} status={status}>
                <Button variant="outline" className="h-10 md:h-9"><Plus />自分で追加</Button>
              </AddItemDialog>
            </div>
            {showSuggest && <Suggestions term={term.year_term} />}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function Rows({ children }: { children: ReactNode }) {
  return <ul className="-mx-1 divide-y">{children}</ul>
}

function Row({ title, sub, lead, children, className }: { title: string; sub?: ReactNode; lead?: ReactNode; children?: ReactNode; className?: string }) {
  return (
    <li className={cn('flex min-h-11 items-center gap-2.5 px-1 py-1.5', className)}>
      {lead}
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm" title={title}>{title}</p>
        {sub && <p className="truncate text-xs text-muted-foreground">{sub}</p>}
      </div>
      {children}
    </li>
  )
}

function Empty({ children }: { children: ReactNode }) {
  return <p className="rounded-md border border-dashed py-4 text-center text-sm text-muted-foreground">{children}</p>
}

/** 「専門教育科目（知能情報関連）」→「知能情報関連」 */
function shortCategory(c: string) {
  const m = /[（(]([^）)]+)[）)]/.exec(c)
  return m ? m[1] : c
}

/** 評価。不合格だけアイコンと色で示す。 */
function GradeMark({ course }: { course: TakenCourse }) {
  if (!course.passed) {
    return (
      <span className="flex w-8 items-center justify-end gap-0.5 text-sm font-medium text-critical" aria-label={`不合格（${course.grade || '評価なし'}）`}>
        <XCircle className="size-3.5" />{course.grade || '-'}
      </span>
    )
  }
  return <span className="w-8 text-right text-sm font-medium tabular-nums">{course.grade || '-'}</span>
}

/** 今期の履修登録。区分が分からない科目は、その場で区分を選んで保存できる。 */
function CurrentLectures({ courses, status }: { courses: TakenCourse[]; status: GraduationStatus }) {
  const save = useSaveLectureCategories()
  const [picked, setPicked] = useState<Record<string, string>>({})
  const items = status.requirements.filter((r) => r.kind === 'item')
  const unmapped = courses.filter((c) => c.unmapped && c.lecture_code)
  if (courses.length === 0) return <Empty>今期の履修登録はありません</Empty>

  const choice = (c: TakenCourse) => picked[c.lecture_code!] ?? c.suggested ?? ''
  return (
    <div className="space-y-2">
      <Rows>
        {courses.map((c, i) =>
          c.unmapped && c.lecture_code ? (
            <li key={i} className="space-y-1.5 px-1 py-2">
              <div className="flex items-center gap-2">
                <AlertTriangle className="size-4 shrink-0 text-warning" aria-label="区分が分からない" />
                <p className="min-w-0 flex-1 truncate text-sm">{c.title}</p>
                <span className="text-xs tabular-nums text-muted-foreground">{credits(c.credits)}</span>
              </div>
              <Select value={choice(c)} onValueChange={(v) => setPicked((p) => ({ ...p, [c.lecture_code!]: v }))}>
                <SelectTrigger className="h-10 w-full md:h-8" aria-label={`${c.title} の区分`}>
                  <SelectValue placeholder="区分を選ぶ" />
                </SelectTrigger>
                <SelectContent>
                  {items.map((r) => <SelectItem key={r.section + r.name} value={r.name}>{r.name}（{r.section}）</SelectItem>)}
                </SelectContent>
              </Select>
            </li>
          ) : (
            <Row key={i} title={c.title} sub={c.category && shortCategory(c.category)}>
              <span className="text-xs tabular-nums text-muted-foreground">{credits(c.credits)}</span>
            </Row>
          ),
        )}
      </Rows>
      {unmapped.length > 0 && (
        <div className="flex items-center gap-2 rounded-md bg-muted p-2.5">
          <p className="flex-1 text-xs text-muted-foreground">区分が分からない科目は、選んで保存するまでグラフに入りません</p>
          <Button
            size="sm"
            disabled={save.isPending || unmapped.every((c) => !choice(c))}
            onClick={() => save.mutate(Object.fromEntries(unmapped.filter((c) => choice(c)).map((c) => [c.lecture_code!, choice(c)])))}
          >
            保存
          </Button>
        </div>
      )}
    </div>
  )
}

function PlanItems({ term }: { term: PlanYearTerm }) {
  const del = useDeletePlanItem()
  return (
    <div>
      {term.state !== 'future' && <p className="mb-1 text-xs text-muted-foreground">計画</p>}
      <Rows>
        {term.items.map((it) => (
          <Row key={it.id} title={it.title} sub={it.category} lead={<span className="size-2 shrink-0 rounded-[2px] bg-planned" aria-hidden />}>
            <span className="text-xs tabular-nums text-muted-foreground">{credits(it.credits)}</span>
            <Button
              size="icon"
              variant="ghost"
              className="-mr-1 size-9 text-muted-foreground"
              aria-label={`${it.title} を計画から外す`}
              disabled={del.isPending}
              onClick={() => del.mutate(it)}
            >
              <X />
            </Button>
          </Row>
        ))}
      </Rows>
    </div>
  )
}
