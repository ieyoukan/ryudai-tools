import { AlertTriangle, CalendarDays, CheckCircle2, ExternalLink, Plus, Search } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Link, useLocation, useSearch } from 'wouter'

import type { PlanCheck } from '@/api/gen/academic'
import type { RegisteredLecture } from '@/api/gen/kyomu'
import type { RegistrationData } from '@/api/gen/ui'
import { usePage } from '@/api/page'
import { ErrorState, LoadingBlock, Notice, PageHeader, SectionTitle, Stat } from '@/components/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { credits } from '@/lib/format'
import { cn } from '@/lib/utils'

/** 今期の履修登録: 単位と履修上限、時間割、追加したい講義のチェック。 */
export function RegistrationPage() {
  const search = useSearch()
  const [, navigate] = useLocation()
  const { data, error, isPending, refetch } = usePage<RegistrationData>('/registration' + (search ? '?' + search : ''))
  const reg = data?.Overview?.registration
  const cs = reg?.credits
  const [todayIdx] = useState(() => new Date().getDay() - 1)

  const check = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const codes = String(new FormData(e.currentTarget).get('plan') ?? '').trim()
    navigate(codes ? '/registration?plan=' + encodeURIComponent(codes) : '/registration')
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title={reg?.year_term?.trim() ? `${reg.year_term.trim()}の履修登録` : '履修登録'}
        actions={reg && <Badge variant={reg.open ? 'default' : 'outline'}>{reg.open ? '履修登録を編集できます' : '履修登録の編集はできません'}</Badge>}
      />
      {isPending && <LoadingBlock />}
      {error && <ErrorState message={error.message} onRetry={() => refetch()} />}
      {data?.Error && <ErrorState message={data.Error} onRetry={() => refetch()} />}

      {data?.Status && data.Status.state !== 'open' && (
        <Card>
          <CardContent className="space-y-4">
            <p className="text-sm">{data.Status.state === 'closed' ? '現在は履修登録期間外です。授業の日程や教室は時間割から確認できます。' : '履修登録期間を確認できませんでした。教務システムで確認するか、もう一度読み込んでください。'}</p>
            <div className="flex flex-wrap gap-2">
              <Button asChild><Link href="/courses"><CalendarDays />時間割へ</Link></Button>
              {data.RegistrationURL && <Button asChild variant="outline"><a href={data.RegistrationURL} target="_blank" rel="noreferrer"><ExternalLink />教務の履修時間割表</a></Button>}
              {data.Status.state === 'unknown' && <Button variant="outline" onClick={() => refetch()}>再読み込み</Button>}
            </div>
          </CardContent>
        </Card>
      )}

      {reg && (
        <>
          {cs && (
            <Card>
              <CardContent className="space-y-3">
                <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
                  <Stat label="履修制限対象" value={credits(cs.limited_credits)} unit={`/ ${credits(cs.limit)} 単位`} />
                  <Stat label="上限まで残り" value={<span className={cn(cs.remaining_credit < 0 && 'text-critical')}>{credits(cs.remaining_credit)}</span>} unit="単位" />
                  <Stat label="対象外科目" value={credits(cs.exempt_credits)} unit="単位" />
                  <Stat label="合計登録" value={credits(cs.total_credits)} unit="単位" />
                </div>
                <div className="space-y-1">
                  <div className="h-2.5 overflow-hidden rounded-[4px] bg-track" role="img" aria-label={`履修上限 ${credits(cs.limit)} 単位のうち ${credits(cs.limited_credits)} 単位`}>
                    <div className={cn('h-full rounded-r-[4px]', cs.remaining_credit < 0 ? 'bg-critical' : 'bg-primary')} style={{ width: `${Math.min(100, (cs.limited_credits / Math.max(cs.limit, 1)) * 100)}%` }} />
                  </div>
                  <p className="text-right text-xs text-muted-foreground">履修上限 {credits(cs.limit)} 単位</p>
                </div>
              </CardContent>
            </Card>
          )}

          {(data.Overview?.notes ?? []).length > 0 && (
            <Notice>{data.Overview!.notes.map((n, i) => <p key={i}>{n}</p>)}</Notice>
          )}

          <Card>
            <CardContent className="space-y-3">
              <SectionTitle actions={<Button asChild size="sm" variant="outline"><Link href="/registration/candidates?go=1"><Search />空きコマの候補を探す</Link></Button>}>時間割</SectionTitle>
              {/* パソコン: 表 */}
              <div className="hidden overflow-x-auto md:block">
                <table className="w-full table-fixed text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="w-12 py-2" />
                      {data.Days.map((d, i) => <th key={d} className={cn('py-2 text-xs font-medium', i === todayIdx ? 'text-primary' : 'text-muted-foreground')}>{d}</th>)}
                    </tr>
                  </thead>
                  <tbody>
                    {data.Rows.map((row) => (
                      <tr key={row.Period} className="border-b last:border-0">
                        <th className="py-2 text-xs font-normal text-muted-foreground">{row.Period}限</th>
                        {row.Cells.map((cell) => (
                          <td key={cell.Day} className="h-20 border-l p-1 align-top">
                            {(cell.Lectures ?? []).length > 0 ? (
                              <div className="space-y-1">{cell.Lectures.map((l) => <LectureChip key={l.lecture_code + l.rank} l={l} room={data.Overview?.lecture_rooms?.[l.lecture_code]} />)}</div>
                            ) : (
                              <Link href={`/registration/candidates?slots=${encodeURIComponent(cell.Day + cell.Period)}&go=1`} className="flex h-full min-h-16 items-center justify-center rounded-md text-xs text-muted-foreground/0 transition-colors hover:bg-muted hover:text-muted-foreground">
                                <Plus className="size-3.5" />候補
                              </Link>
                            )}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {/* スマホ: 曜日ごと。授業のあるコマだけ並べ、空きコマはまとめて 1 行にする */}
              <div className="space-y-3 md:hidden">
                {data.Days.map((d, i) => {
                  const filled = data.Rows.filter((row) => (row.Cells[i]?.Lectures ?? []).length > 0)
                  const empty = data.Rows.filter((row) => (row.Cells[i]?.Lectures ?? []).length === 0).map((row) => row.Period)
                  return (
                    <section key={d}>
                      <h3 className={cn('mb-1 text-xs font-medium text-muted-foreground', i === todayIdx && 'text-primary')}>{d}曜{i === todayIdx && '（今日）'}</h3>
                      <ul className="divide-y rounded-lg border">
                        {filled.map((row) => (
                          <li key={row.Period} className="flex min-h-12 items-center gap-3 px-3 py-1.5">
                            <span className="w-7 text-xs tabular-nums text-muted-foreground">{row.Period}限</span>
                            <div className="min-w-0 flex-1 space-y-1">{row.Cells[i].Lectures.map((l) => <LectureChip key={l.lecture_code + l.rank} l={l} room={data.Overview?.lecture_rooms?.[l.lecture_code]} />)}</div>
                          </li>
                        ))}
                        {empty.length > 0 && (
                          <li>
                            <Link href={`/registration/candidates?slots=${encodeURIComponent(empty.map((p) => d + p).join(' '))}&go=1`} className="flex min-h-11 items-center gap-2 px-3 text-xs text-muted-foreground active:bg-muted">
                              <Plus className="size-3.5" />空き {empty.map((p) => `${p}限`).join('・')} ・ 候補を見る
                            </Link>
                          </li>
                        )}
                      </ul>
                    </section>
                  )
                })}
              </div>
              {(data.Intensive ?? []).length > 0 && (
                <div className="space-y-1">
                  <h3 className="text-sm font-medium">集中講義など</h3>
                  <ul className="divide-y rounded-lg border">
                    {data.Intensive.map((l) => <li key={l.lecture_code} className="px-3 py-2"><LectureChip l={l} /></li>)}
                  </ul>
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardContent className="space-y-3">
              <SectionTitle>追加したい講義をチェック</SectionTitle>
              <p className="text-xs text-muted-foreground">講義コードを入れると、時間割の重なり・履修上限・定員・履修条件を確かめます（空白区切りで 20 件まで）。</p>
              <form onSubmit={check} className="flex gap-2" key={search}>
                <Input name="plan" defaultValue={data.PlanCodes} placeholder="例: 123456789 234567890" className="h-10 md:h-9" inputMode="numeric" />
                <Button type="submit" className="h-10 md:h-9">チェック</Button>
              </form>
              {data.PlanError && <Notice tone="warning">{data.PlanError}</Notice>}
              {data.Plan && <PlanResult plan={data.Plan} />}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}

function LectureChip({ l, room }: { l: RegisteredLecture; room?: string }) {
  return (
    <Link href={`/syllabus/${l.year || ''}/${l.lecture_code}`} className={cn('block rounded-md px-2 py-1 text-xs leading-snug hover:bg-muted', l.rank > 1 && 'border border-dashed text-muted-foreground')}>
      {l.rank > 1 && <span className="mr-1 text-[10px]">第{l.rank}希望</span>}
      <span className="font-medium text-foreground">{l.title}</span>
      <span className="block text-[11px] text-muted-foreground">
        {[`${credits(l.credits)}単位`, room, l.lot_result && `抽選: ${l.lot_result}`, l.adjustment && `調整: ${l.adjustment}`].filter(Boolean).join(' ・ ')}
      </span>
    </Link>
  )
}

function PlanResult({ plan }: { plan: PlanCheck }) {
  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <div className="flex justify-between text-xs text-muted-foreground">
          <span>追加後 {credits(plan.projected_credits)} 単位（今 {credits(plan.current_credits)} ＋ 追加 {credits(plan.added_credits)}）</span>
          <span className={cn(plan.over_limit && 'flex items-center gap-1 font-medium text-critical')}>{plan.over_limit && <AlertTriangle className="size-3.5" />}上限 {credits(plan.limit)}</span>
        </div>
        <div className="flex h-2.5 overflow-hidden rounded-[4px] bg-track">
          <div className="h-full bg-primary" style={{ width: `${Math.min(100, (plan.current_credits / Math.max(plan.limit, 1)) * 100)}%` }} />
          <div className={cn('h-full border-l-2 border-card', plan.over_limit ? 'bg-critical' : 'bg-primary/45')} style={{ width: `${Math.min(100, (plan.added_credits / Math.max(plan.limit, 1)) * 100)}%` }} />
        </div>
      </div>
      <ul className="divide-y rounded-lg border">
        {plan.items.map((it) => {
          const ok = !it.error && !(it.conflicts ?? []).length
          return (
            <li key={it.lecture_code} className="flex items-start gap-2 px-3 py-2 text-sm">
              {ok ? <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-good" /> : <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />}
              <div className="min-w-0 flex-1 space-y-0.5">
                <p className="font-medium">{it.title || it.lecture_code}<span className="ml-2 text-xs font-normal text-muted-foreground">{[it.day_period, `${credits(it.credits)}単位`, it.registration_method, it.already_registered && '登録済み'].filter(Boolean).join(' ・ ')}</span></p>
                {it.error && <p className="text-xs text-critical">{it.error}</p>}
                {(it.conflicts ?? []).length > 0 && <p className="text-xs text-warning">重なり: {it.conflicts!.join(' / ')}</p>}
                {(it.eligibility_notes ?? []).map((n, i) => <p key={i} className="text-xs text-muted-foreground">注意: {n}</p>)}
                {it.availability && <p className="text-xs text-muted-foreground">定員 {it.availability.capacity ?? '不明'}{it.availability.seats_left != null && ` ・ 残り ${it.availability.seats_left}`}</p>}
              </div>
            </li>
          )
        })}
      </ul>
      {plan.notes.length > 0 && <Notice>{plan.notes.map((n, i) => <p key={i}>{n}</p>)}</Notice>}
    </div>
  )
}
