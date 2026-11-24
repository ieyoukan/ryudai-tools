import { Search, SlidersHorizontal } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Link, useLocation, useSearch } from 'wouter'

import type { SyllabusSearchData } from '@/api/gen/ui'
import type { Option } from '@/api/gen/kyomu'
import { usePage } from '@/api/page'
import { EmptyState, ErrorState, LoadingBlock, PageHeader } from '@/components/page'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { credits } from '@/lib/format'
import { SeatInfo } from './lectureParts'

const fields = ['subject', 'keyword', 'instructor', 'lecture_code', 'year', 'term', 'faculty', 'department', 'day', 'period'] as const

/** シラバス検索（ログイン不要）。条件は URL に入れるので、検索結果をそのまま共有・戻るができる。 */
export function SyllabusPage() {
  const search = useSearch()
  const [, navigate] = useLocation()
  const params = new URLSearchParams(search)
  const hasQuery = fields.some((f) => f !== 'year' && params.get(f))
  const { data, error, isPending, refetch } = usePage<SyllabusSearchData>('/syllabus' + (search ? '?' + search : ''), { staleTime: 5 * 60_000 })
  const [more, setMore] = useState(fields.slice(2).some((f) => params.get(f) && f !== 'year'))

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const q = new URLSearchParams()
    for (const f of fields) {
      const v = String(form.get(f) ?? '').trim()
      if (v) q.set(f, v)
    }
    navigate('/syllabus?' + q.toString())
  }
  const opts = data?.Options

  return (
    <div className="space-y-4">
      <PageHeader title="シラバス検索" description={!data?.Username && <><Link href="/login?next=/syllabus" className="underline underline-offset-4">ログイン</Link>すると、定員・登録者数と自分の登録状況も見られます。</>} />
      <Card>
        <CardContent>
          <form onSubmit={submit} className="space-y-3" key={search}>
            <div className="flex gap-2">
              <Input name="subject" defaultValue={params.get('subject') ?? ''} placeholder="科目名（例: プログラミング）" className="h-10 md:h-9" aria-label="科目名" />
              <Button type="submit" className="h-10 md:h-9"><Search />検索</Button>
            </div>
            <Button type="button" variant="ghost" size="sm" className="text-muted-foreground" onClick={() => setMore((v) => !v)} aria-expanded={more}>
              <SlidersHorizontal />条件を{more ? '閉じる' : '追加'}
            </Button>
            {more && (
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <Field label="キーワード（全文）" name="keyword" params={params} />
                <Field label="教員名" name="instructor" params={params} />
                <Field label="講義コード" name="lecture_code" params={params} />
                <Field label="開講学科" name="department" params={params} placeholder="例: 工学科（学部と一緒に）" />
                <SelectField label="年度" name="year" params={params} options={opts?.years} noneLabel="今年度" />
                <SelectField label="学期" name="term" params={params} options={opts?.terms} />
                <SelectField label="開講学部" name="faculty" params={params} options={opts?.faculties} />
                <SelectField label="曜日" name="day" params={params} options={opts?.days} />
                <SelectField label="時限" name="period" params={params} options={opts?.periods} />
              </div>
            )}
          </form>
        </CardContent>
      </Card>

      {hasQuery && isPending && <LoadingBlock />}
      {error && <ErrorState message={error.message} onRetry={() => refetch()} />}
      {data?.Error && <ErrorState message={data.Error} />}
      {data?.Result && (
        <div className="space-y-2">
          <p className="text-sm text-muted-foreground">{data.Result.total} 件{data.Result.truncated && `（先頭 ${data.Result.returned} 件。条件を絞ると全件見られます）`}</p>
          {(data.Result.results ?? []).length === 0 ? <EmptyState>見つかりませんでした。</EmptyState> : (
            <ul className="divide-y rounded-xl border bg-card">
              {data.Result.results.map((r) => (
                <li key={r.year + r.lecture_code}>
                  <Link href={`/syllabus/${r.year}/${r.lecture_code}`} className="block px-4 py-3 hover:bg-muted/50">
                    <div className="flex items-baseline justify-between gap-2">
                      <p className="font-medium">{r.title}</p>
                      {r.credits > 0 && <span className="shrink-0 text-sm tabular-nums">{credits(r.credits)}<span className="text-xs text-muted-foreground"> 単位</span></span>}
                    </div>
                    <p className="text-xs text-muted-foreground">{[r.term, r.day_period, r.instructors, r.faculty, r.registration_method, r.teaching_format].filter(Boolean).join(' ・ ')}</p>
                    {r.availability && <SeatInfo a={r.availability} className="mt-1" />}
                    {r.outline_excerpt && <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{r.outline_excerpt}</p>}
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

function Field({ label, name, params, placeholder }: { label: string; name: string; params: URLSearchParams; placeholder?: string }) {
  return (
    <div className="space-y-1">
      <Label htmlFor={'f-' + name} className="text-xs text-muted-foreground">{label}</Label>
      <Input id={'f-' + name} name={name} defaultValue={params.get(name) ?? ''} placeholder={placeholder} className="h-10 md:h-9" />
    </div>
  )
}

/** 選択肢が取れなかったときは入力欄にする。 */
function SelectField({ label, name, params, options, noneLabel = '指定なし' }: { label: string; name: string; params: URLSearchParams; options?: Option[]; noneLabel?: string }) {
  if (!options) return <Field label={label} name={name} params={params} />
  return (
    <div className="space-y-1">
      <Label htmlFor={'f-' + name} className="text-xs text-muted-foreground">{label}</Label>
      <select
        id={'f-' + name}
        name={name}
        defaultValue={params.get(name) ?? ''}
        className="h-10 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50 md:h-9 dark:bg-input/30"
      >
        <option value="">{noneLabel}</option>
        {options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
    </div>
  )
}
