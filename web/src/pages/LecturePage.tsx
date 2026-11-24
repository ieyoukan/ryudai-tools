import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BookOpen, CalendarClock, ExternalLink, MapPin, RefreshCw, User } from 'lucide-react'
import { Link, useLocation, useParams, useSearch } from 'wouter'

import { api } from '@/api/client'
import type { LectureDetails, LectureSchedule, Location } from '@/api/gen/academic'
import type { LectureSession } from '@/api/gen/kyomu'
import type { CoursesData, SyllabusDetailData } from '@/api/gen/ui'
import { useMe, usePage } from '@/api/page'
import { BackLink } from '@/components/BackLink'
import { CampusMap } from '@/components/CampusMap'
import { EmptyState, ErrorState, LoadingBlock, PageHeader } from '@/components/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { durationLabel, fetchedLabel, fullDate, minutesBetween, nowClockInJapan, shortDate, todayInJapan, validDate, webClassCourseID } from '@/lib/schedule'
import { cn } from '@/lib/utils'
import { CourseContents } from './ContentsPage'
import { LectureSyllabus } from './syllabusParts'

type Tab = 'kyomu' | 'webclass'

/** 講義1件。教務システム（全授業日程）と WebClass（資料・出席確認）をタブで切り替える。 */
export function LecturePage() {
  const { year: yearParam, code } = useParams<{ year: string; code: string }>()
  const year = Number(yearParam)
  const search = new URLSearchParams(useSearch())
  const [path, navigate] = useLocation()
  const tab: Tab = search.get('tab') === 'webclass' ? 'webclass' : 'kyomu'
  const date = validDate(search.get('date') ?? '') ? search.get('date')! : ''
  const { data: me } = useMe()
  const apiPath = `/api/me/lectures/${year}/${encodeURIComponent(code)}`
  const queryKey = ['lecture', me?.Username ?? '', year, code]
  const qc = useQueryClient()
  const query = useQuery({ queryKey, queryFn: () => api.get<LectureSchedule>(apiPath), enabled: me !== undefined && Number.isInteger(year), staleTime: 10 * 60_000, retry: false })
  const refresh = useMutation({ mutationFn: () => api.get<LectureSchedule>(apiPath + '?refresh=1'), onSuccess: (d) => qc.setQueryData(queryKey, d) })
  const data = query.data
  // 公開シラバスを独立して取得し、授業日程や WebClass の読み込みを待たせない。
  const syllabusQuery = usePage<SyllabusDetailData>(`/syllabus/${year}/${encodeURIComponent(code)}?availability=0`, { enabled: Number.isInteger(year) && tab === 'kyomu', staleTime: 10 * 60_000 })
  const syllabus = syllabusQuery.data?.Syllabus
  const title = data?.title || syllabus?.title || search.get('name') || code

  const setTab = (next: string) => {
    const q = new URLSearchParams(search)
    if (next === 'webclass') q.set('tab', 'webclass')
    else q.delete('tab')
    const qs = q.toString()
    navigate(path + (qs ? '?' + qs : ''), { replace: true })
  }

  const meta = data ? [data.number_code, data.day_period, data.term, data.credits && `${data.credits}単位`, data.instructors].filter(Boolean).join(' · ') : ''

  return (
    <div className="space-y-4">
      <PageHeader back={<BackLink href="/courses">時間割</BackLink>} title={title} description={meta || undefined} />
      <Tabs value={tab} onValueChange={setTab} className="gap-4">
        <TabsList className="w-full sm:w-auto">
          <TabsTrigger value="kyomu" className="px-4">教務システム</TabsTrigger>
          <TabsTrigger value="webclass" className="px-4">WebClass</TabsTrigger>
        </TabsList>
        <TabsContent value="kyomu" className="space-y-4">
          {query.isPending && <LoadingBlock label="教務システムから授業日程を読み込んでいます" />}
          {query.error && <ErrorState message={query.error.message} onRetry={() => query.refetch()} />}
          {refresh.error && <ErrorState message={`更新に失敗しました。${refresh.error.message}`} onRetry={() => refresh.mutate()} />}
          {data && <KyomuView data={data} syllabus={syllabus} date={date} busy={query.isFetching || refresh.isPending} onRefresh={() => refresh.mutate()} />}
          {syllabusQuery.isPending && <LoadingBlock label="シラバスの授業計画・評価方法・教科書を読み込んでいます" />}
          {syllabusQuery.error && <ErrorState message={`シラバスを取得できませんでした。${syllabusQuery.error.message}`} onRetry={() => syllabusQuery.refetch()} />}
          {syllabusQuery.data?.Error && <ErrorState message={syllabusQuery.data.Error} onRetry={() => syllabusQuery.refetch()} />}
          {syllabus && <LectureSyllabus syllabus={syllabus} />}
        </TabsContent>
        <TabsContent value="webclass">
          <WebClassView year={year} code={code} title={title} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function KyomuView({ data, syllabus, date, busy, onRefresh }: { data: LectureSchedule; syllabus?: LectureDetails; date: string; busy: boolean; onRefresh: () => void }) {
  const today = todayInJapan()
  const clock = nowClockInJapan()
  const sessions = data.sessions ?? []
  const isOver = (s: LectureSession) => s.date < today || (s.date === today && !!s.end_time && s.end_time <= clock)
  const upcoming = sessions.find((s) => !isOver(s) && !s.cancelled)
  const picked = date ? sessions.find((s) => s.date === date) : undefined
  const focus = picked ?? upcoming
  const done = sessions.filter((s) => isOver(s) && !s.cancelled).length
  const held = sessions.filter((s) => !s.cancelled).length
  const cancelled = sessions.filter((s) => s.cancelled).length
  const plan = new Map((syllabus?.plan_sessions ?? []).map((s) => [s.no, s.content]))
  const hasPlan = sessions.some((s) => plan.has(s.no))

  return (
    <>
      {focus ? (
        <div className="grid gap-3 md:grid-cols-5">
          <FocusCard session={focus} label={picked ? '選んだ日の授業' : '次回の授業'} total={sessions.length} over={isOver(focus)} today={today} clock={clock} location={data.locations?.[focus.room ?? '']} className="md:col-span-2" />
          <CampusMap location={data.locations?.[focus.room ?? '']} room={focus.room} className="h-56 md:col-span-3 md:h-auto" />
        </div>
      ) : sessions.length > 0 ? (
        <EmptyState icon={<CalendarClock className="size-5" />}>この講義の授業はすべて終わりました。</EmptyState>
      ) : null}

      {sessions.length > 0 && (
        <div className="grid grid-cols-3 gap-3">
          <MiniStat label="実施済み" value={`${done}`} unit={`/ ${held}回`} />
          <MiniStat label="残り" value={`${held - done}`} unit="回" />
          <MiniStat label="休講" value={`${cancelled}`} unit="回" tone={cancelled > 0 ? 'critical' : undefined} />
        </div>
      )}

      <section className="rounded-xl border bg-card" aria-label="授業明細スケジュール">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-2">
          <h2 className="text-sm font-semibold">授業明細スケジュール <span className="ml-1 font-normal text-muted-foreground">全{sessions.length}回</span></h2>
          <Button variant="ghost" size="sm" disabled={busy} onClick={onRefresh}><RefreshCw className={cn(busy && 'animate-spin')} />更新</Button>
        </div>
        {sessions.length === 0 ? (
          <p className="px-4 py-6 text-sm text-muted-foreground">教務システムに授業日程が登録されていません（集中講義などは日程が後から登録されることがあります）。</p>
        ) : (
          <>
            {/* パソコン: 表 */}
            <table className="hidden w-full text-sm md:table">
              <thead>
                <tr className="border-b bg-muted/40 text-xs text-muted-foreground">
                  <th className="w-10 py-2 font-normal">回</th>
                  <th className="w-32 py-2 text-left font-normal">日付</th>
                  <th className="w-36 py-2 text-left font-normal">時刻</th>
                  <th className="w-28 py-2 text-left font-normal">教室</th>
                  {hasPlan && <th className="py-2 pr-3 text-left font-normal">授業計画</th>}
                  <th className="w-28 py-2 text-left font-normal">担当教員</th>
                  <th className="py-2 pr-4 text-left font-normal">備考</th>
                </tr>
              </thead>
              <tbody>
                {sessions.map((s) => (
                  <tr key={s.no + s.date} className={cn('border-b last:border-0', isOver(s) && 'text-muted-foreground', s === focus && 'bg-primary/5', s.date === today && 'font-medium')}>
                    <td className="py-2 text-center tabular-nums">{s.no}</td>
                    <td className={cn('py-2 tabular-nums', s.cancelled && 'line-through')}>{shortDate(s.date)}{s.date === today && <Badge className="ml-2">今日</Badge>}{s === upcoming && s.date !== today && <Badge variant="outline" className="ml-2">次回</Badge>}</td>
                    <td className="py-2 tabular-nums">{s.start_time ? `${s.start_time} – ${s.end_time}` : '—'}<span className="ml-1 text-xs text-muted-foreground">{s.period}</span></td>
                    <td className="py-2">{s.room || '—'}</td>
                    {hasPlan && <td className="whitespace-pre-wrap break-words py-2 pr-3 text-xs leading-relaxed">{plan.get(s.no) || '—'}</td>}
                    <td className="py-2">{s.instructors}</td>
                    <td className={cn('whitespace-pre-line py-2 pr-4 text-xs', s.cancelled && 'font-medium text-critical')}>{s.note}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {/* スマホ: リスト */}
            <ul className="divide-y md:hidden">
              {sessions.map((s) => (
                <li key={s.no + s.date} className={cn('flex gap-3 px-4 py-2.5', isOver(s) && 'text-muted-foreground', s === focus && 'bg-primary/5')}>
                  <span className="w-6 shrink-0 pt-0.5 text-right text-xs tabular-nums text-muted-foreground">{s.no}</span>
                  <div className="min-w-0 flex-1">
                    <p className={cn('text-sm tabular-nums', s.cancelled && 'line-through')}>
                      {shortDate(s.date)} <span className="ml-1">{s.start_time ? `${s.start_time} – ${s.end_time}` : s.period}</span>
                      {s.date === today && <Badge className="ml-2">今日</Badge>}
                      {s === upcoming && s.date !== today && <Badge variant="outline" className="ml-2">次回</Badge>}
                    </p>
                    <p className="text-xs text-muted-foreground">{[s.room, s.instructors].filter(Boolean).join(' · ')}</p>
                    {plan.has(s.no) && <p className="mt-1 whitespace-pre-wrap break-words text-sm leading-relaxed"><span className="mr-1.5 text-xs text-muted-foreground">授業計画</span>{plan.get(s.no)}</p>}
                    {s.note && <p className={cn('whitespace-pre-line text-xs', s.cancelled ? 'font-medium text-critical' : 'text-muted-foreground')}>{s.note}</p>}
                  </div>
                </li>
              ))}
            </ul>
          </>
        )}
      </section>

      {hasPlan && <p className="text-xs leading-relaxed text-muted-foreground">授業計画はシラバスに明記された回番号を対応させて表示しています。日程変更などで実際の内容と異なる場合があります。</p>}

      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">教務システムの「授業明細スケジュール」から取得（日本時間 · 取得 {fetchedLabel(data.fetched_at)}、最大10分間保存）</p>
        <div className="flex flex-wrap gap-2">
          <Button asChild variant="outline" size="sm"><Link href={`/syllabus/${data.year}/${encodeURIComponent(data.lecture_code)}`}><BookOpen />シラバス</Link></Button>
          <Button asChild variant="outline" size="sm"><a href={data.source} target="_blank" rel="noreferrer"><ExternalLink />教務システムで開く</a></Button>
        </div>
      </div>
    </>
  )
}

function FocusCard({ session: s, label, total, over, today, clock, location: l, className }: { session: LectureSession; label: string; total: number; over: boolean; today: string; clock: string; location?: Location; className?: string }) {
  let when = ''
  if (s.date === today && s.start_time && s.end_time) {
    if (clock < s.start_time) when = `あと ${durationLabel(minutesBetween(clock, s.start_time))}`
    else if (clock < s.end_time) when = `授業中 · 残り ${durationLabel(minutesBetween(clock, s.end_time))}`
  } else if (s.date > today) {
    const days = Math.round((Date.parse(s.date) - Date.parse(today)) / 86_400_000)
    when = days === 1 ? '明日' : `${days}日後`
  }
  return (
    <div className={cn('rounded-xl border bg-card p-4', s.cancelled && 'bg-muted/40', className)}>
      <div className="flex items-center justify-between gap-2">
        <p className="flex items-center gap-1.5 text-xs font-medium text-primary"><CalendarClock className="size-3.5" />{label} · 第{s.no}回 / 全{total}回</p>
        {when && !over && <span className="text-xs tabular-nums text-muted-foreground">{when}</span>}
      </div>
      <p className={cn('mt-2 text-lg font-semibold', s.cancelled && 'text-muted-foreground line-through')}>
        {fullDate(s.date)} <span className="tabular-nums">{s.start_time ? `${s.start_time} – ${s.end_time}` : ''}</span>
        {s.period && <span className="ml-2 text-sm font-normal text-muted-foreground">{s.period}</span>}
      </p>
      {s.room && (
        <div className="mt-3 flex items-start gap-1.5">
          <MapPin className="mt-0.5 size-4 shrink-0 text-primary" />
          <div>
            <p className="text-base font-semibold">{s.room}</p>
            {l && !l.online && <p className="text-sm text-muted-foreground">{l.building}{l.detail ? ` ${l.detail}` : l.floor ? ` ${l.floor}階` : ''}</p>}
          </div>
        </div>
      )}
      {s.instructors && <p className="mt-2 flex items-center gap-1.5 text-sm text-muted-foreground"><User className="size-4" />{s.instructors}</p>}
      {s.note && <p className={cn('mt-2 whitespace-pre-line text-sm', s.cancelled ? 'font-medium text-critical' : 'text-muted-foreground')}>{s.note}</p>}
    </div>
  )
}

function MiniStat({ label, value, unit, tone }: { label: string; value: string; unit: string; tone?: 'critical' }) {
  return (
    <div className="rounded-xl border bg-card p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={cn('mt-0.5 text-xl font-semibold tabular-nums', tone === 'critical' && 'text-critical')}>{value}<span className="ml-1 text-xs font-normal text-muted-foreground">{unit}</span></p>
    </div>
  )
}

/** 教務の講義に対応する WebClass のコースを時間割の一覧から探して、資料と出席確認を出す。 */
function WebClassView({ year, code, title }: { year: number; code: string; title: string }) {
  const { data, error, isPending, refetch } = usePage<CoursesData>('/courses')
  if (isPending) return <LoadingBlock label="WebClass のコースを探しています" />
  if (error) return <ErrorState message={error.message} onRetry={() => refetch()} />
  if (data?.Error) return <ErrorState message={data.Error} onRetry={() => refetch()} />
  const courses = (data?.Rows ?? []).flatMap((r) => r.Cells.flatMap((c) => (c ?? []).filter((x): x is NonNullable<typeof x> => !!x)))
  const id = webClassCourseID(year, code)
  const course = courses.find((c) => c.id === id) ?? courses.find((c) => c.name.startsWith(title + '['))
  if (!course) {
    return (
      <EmptyState>
        WebClass にこの講義のコースが見つかりませんでした。
        <span className="text-xs">WebClass にコースが作られていない講義もあります。時間割の「WebClass のコース」から探すこともできます。</span>
      </EmptyState>
    )
  }
  return <CourseContents courseID={course.id} name={course.name} />
}
