import { AlertTriangle, ListChecks, Search } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Link, useLocation, useSearch } from 'wouter'

import type { CandidateView } from '@/api/gen/academic'
import type { CandidatesData } from '@/api/gen/ui'
import { usePage } from '@/api/page'
import { BackLink } from '@/components/BackLink'
import { EmptyState, ErrorState, LoadingBlock, Notice, PageHeader } from '@/components/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { credits } from '@/lib/format'
import { cn } from '@/lib/utils'

const scopes = [
  { v: 'general', label: '共通教育' },
  { v: 'own', label: '自学部' },
  { v: 'other', label: '他学部' },
]

/** 空きコマ（または指定したコマ）に入れられる講義を探す。まず一覧を出し、履修条件・教室などの補足は続けて読み込む。 */
export function CandidatesPage() {
  const search = useSearch()
  const [, navigate] = useLocation()
  const params = new URLSearchParams(search)
  const searched = params.get('go') === '1'
  const first = usePage<CandidatesData>('/registration/candidates' + (search ? '?' + search : ''))
  const wantDetails = searched && !!first.data?.DetailsDeferred
  const full = usePage<CandidatesData>('/registration/candidates?' + search + '&details=full', { enabled: wantDetails, staleTime: 5 * 60_000 })
  const data = full.data ?? first.data
  const [picked, setPicked] = useState<string[]>([])

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    const q = new URLSearchParams({ go: '1' })
    for (const k of ['slots', 'keywords', 'filter']) {
      const v = String(f.get(k) ?? '').trim()
      if (v) q.set(k, v)
    }
    for (const s of f.getAll('scope')) q.append('scope', String(s))
    if (f.get('full')) q.set('full', '1')
    setPicked([])
    navigate('/registration/candidates?' + q.toString())
  }
  const res = data?.Result

  return (
    <div className="space-y-4">
      <PageHeader back={<BackLink href="/registration">履修登録</BackLink>} title="入れられる講義を探す" />
      <Card>
        <CardContent>
          <form onSubmit={submit} className="space-y-3" key={search}>
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="space-y-1">
                <Label htmlFor="slots" className="text-xs text-muted-foreground">コマ</Label>
                <Input id="slots" name="slots" defaultValue={params.get('slots') ?? ''} placeholder="空欄なら空きコマ全部（例: 月2 木3）" className="h-10 md:h-9" />
              </div>
              <div className="space-y-1">
                <Label htmlFor="keywords" className="text-xs text-muted-foreground">授業内容のキーワード</Label>
                <Input id="keywords" name="keywords" defaultValue={params.get('keywords') ?? ''} placeholder="例: 発声 会話" className="h-10 md:h-9" />
              </div>
              <div className="space-y-1">
                <Label htmlFor="filter" className="text-xs text-muted-foreground">科目名・教員名</Label>
                <Input id="filter" name="filter" defaultValue={params.get('filter') ?? ''} className="h-10 md:h-9" />
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
              {scopes.map((s) => (
                <label key={s.v} className="flex min-h-10 items-center gap-2 md:min-h-0">
                  <input type="checkbox" name="scope" value={s.v} defaultChecked={data ? !!data.Scopes?.[s.v] : s.v !== 'other'} className="size-4 accent-primary" />
                  {s.label}
                </label>
              ))}
              <label className="flex min-h-10 items-center gap-2 md:min-h-0">
                <input type="checkbox" name="full" value="1" defaultChecked={params.get('full') === '1'} className="size-4 accent-primary" />
                定員超過も表示
              </label>
              <Button type="submit" className="ml-auto h-10 md:h-9"><Search />探す</Button>
            </div>
          </form>
        </CardContent>
      </Card>

      {searched && first.isPending && <LoadingBlock />}
      {first.error && <ErrorState message={first.error.message} onRetry={() => first.refetch()} />}
      {data?.Error && <ErrorState message={data.Error} />}

      {res && (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2 text-sm text-muted-foreground">
            <span>
              {res.year_term} ・ {res.searched_slots?.length ?? 0} コマから {res.candidates?.length ?? 0} 件
              {res.remaining_credits != null && ` ・ 上限まで残り ${credits(res.remaining_credits)} 単位`}
            </span>
            {wantDetails && full.isPending && <span className="flex items-center gap-1.5 text-xs"><Skeleton className="size-3 rounded-full" />履修条件・教室を確認中</span>}
          </div>
          {(res.errors ?? []).map((e, i) => <Notice key={i} tone="warning">{e}</Notice>)}
          {(res.timetable_corrections ?? []).map((c, i) => (
            <details key={i} className="rounded-lg border bg-card px-3 py-2 text-sm">
              <summary className="cursor-pointer">時間割訂正: {c.document}（{c.page} ページ）</summary>
              <pre className="mt-2 text-xs whitespace-pre-wrap text-muted-foreground">{c.text}</pre>
              <a href={`${c.source.url}#page=${c.page}`} target="_blank" rel="noopener" className="text-xs underline">公式の訂正表を開く</a>
            </details>
          ))}
          {(res.candidates ?? []).length === 0 ? <EmptyState>条件に合う講義はありませんでした。</EmptyState> : (
            <ul className="space-y-2">
              {res.candidates.map((c) => (
                <Candidate key={c.lecture_code} c={c} checked={picked.includes(c.lecture_code)} onToggle={(on) => setPicked((p) => (on ? [...p, c.lecture_code] : p.filter((x) => x !== c.lecture_code)))} />
              ))}
            </ul>
          )}
        </div>
      )}

      {picked.length > 0 && (
        <div className="fixed inset-x-0 bottom-16 z-20 px-4 pb-[env(safe-area-inset-bottom)] md:bottom-6">
          <div className="mx-auto flex max-w-md items-center gap-3 rounded-xl border bg-card p-2 pl-4 shadow-lg">
            <span className="flex-1 text-sm">{picked.length} 件を選択中</span>
            <Button asChild><Link href={'/registration?plan=' + encodeURIComponent(picked.join(' '))}><ListChecks />履修プランをチェック</Link></Button>
          </div>
        </div>
      )}
    </div>
  )
}

function Candidate({ c, checked, onToggle }: { c: CandidateView; checked: boolean; onToggle: (on: boolean) => void }) {
  const full = c.seats_left != null && c.seats_left <= 0
  return (
    <li className={cn('rounded-xl border bg-card p-3', !c.fits_free_slots && 'border-dashed')}>
      <div className="flex gap-3">
        <input
          type="checkbox"
          className="mt-1 size-5 shrink-0 accent-primary md:size-4"
          checked={checked}
          disabled={c.already_registered}
          onChange={(e) => onToggle(e.target.checked)}
          aria-label={`${c.title} を選ぶ`}
        />
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-baseline gap-x-2">
            <span className="text-xs font-medium tabular-nums text-muted-foreground">{c.day_period}</span>
            <Link href={`/syllabus/${c.year || ''}/${c.lecture_code}`} className="font-medium hover:underline">{c.title}</Link>
            {c.already_registered && <Badge variant="outline">登録済み</Badge>}
            {c.method && <Badge variant="outline">{c.method}</Badge>}
          </div>
          <p className="text-xs text-muted-foreground">{[`${credits(c.credits)}単位`, c.selection, c.instructors, c.faculty, c.lecture_code].filter(Boolean).join(' ・ ')}</p>
          {/* 定員の埋まり具合 */}
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span className="h-1.5 w-24 overflow-hidden rounded-full bg-track" aria-hidden>
              {c.capacity_known && c.capacity > 0 && <span className={cn('block h-full rounded-full', full ? 'bg-critical' : 'bg-primary')} style={{ width: `${Math.min(100, (c.registered / c.capacity) * 100)}%` }} />}
            </span>
            <span className={cn('tabular-nums', full && 'font-medium text-critical')}>
              {c.registered_known ? c.registered : '?'}/{c.capacity_known ? c.capacity : '?'}{full ? ' 満員' : c.seats_left != null ? ` ・ 残り ${c.seats_left}` : ''}
            </span>
          </div>
          {(c.conflicts ?? []).length > 0 && <p className="flex items-center gap-1 text-xs text-warning"><AlertTriangle className="size-3.5" />重なり: {c.conflicts!.join(' / ')}</p>}
          {(c.matched_keywords ?? []).length > 0 && <p className="text-xs text-primary">シラバスに「{c.matched_keywords!.join('」「')}」</p>}
          {(c.neighbors ?? []).map((n, i) => (
            <p key={i} className="text-xs text-muted-foreground">{n.position}のコマ {n.slot}: {n.title}{n.room && `（${n.room}）`}{n.break_minutes ? ` ・ 休み ${n.break_minutes} 分` : ''}</p>
          ))}
          {c.timetable?.notes && <p className="text-xs text-warning">時間割の注意: {c.timetable.notes}</p>}
          {c.syllabus && (
            <div className="space-y-0.5 text-xs text-muted-foreground">
              {c.syllabus.error && <p>シラバスの補足を取得できませんでした: {c.syllabus.error}</p>}
              {c.syllabus.room && <p>教室: {c.syllabus.room}</p>}
              {c.syllabus.outline && <p className="line-clamp-3">{c.syllabus.outline}</p>}
              {(c.syllabus.workload_signals ?? []).length > 0 && <p>負担の目安: {c.syllabus.workload_signals!.join('・')}</p>}
              {(c.syllabus.eligibility_notes ?? []).map((n, i) => <p key={i} className="text-warning">注意: {n}</p>)}
            </div>
          )}
        </div>
      </div>
    </li>
  )
}
