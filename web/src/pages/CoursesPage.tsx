import { CalendarX } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'wouter'

import type { CoursesData } from '@/api/gen/ui'
import { usePage, useRefreshFromWebClass } from '@/api/page'
import { RefreshButton } from '@/components/RefreshButton'
import { EmptyState, ErrorState, LoadingBlock, PageHeader } from '@/components/page'
import { cn } from '@/lib/utils'
import { ScheduleCalendar } from './ScheduleCalendar'
import { WeekDashboard } from './WeekDashboard'
import { Button } from '@/components/ui/button'

const dayNames: Record<string, string> = { 月: '月曜', 火: '火曜', 水: '水曜', 木: '木曜', 金: '金曜', 土: '土曜' }

const views = [
  { key: 'week', label: '今週' },
  { key: 'calendar', label: 'カレンダー' },
  { key: 'webclass', label: 'WebClass のコース' },
] as const
type View = (typeof views)[number]['key']

/** 時間割。最初は今週の授業（日時・教室）のダッシュボード。講義を押すと教務と WebClass をタブで見られる。 */
export function CoursesPage() {
  const [view, setView] = useState<View>(() => {
    const v = new URLSearchParams(location.search).get('view')
    return views.some((x) => x.key === v) ? (v as View) : 'week'
  })
  const change = (v: View) => {
    setView(v)
    history.replaceState(null, '', v === 'week' ? '/courses' : `/courses?view=${v}`)
  }
  return (
    <div className="space-y-4">
      <PageHeader title="時間割" description="今週の授業の時間と教室。講義を押すと、授業計画・評価方法・教科書や WebClass の資料を確認できます。" />
      <div className="flex flex-wrap gap-1 border-b pb-3" role="group" aria-label="時間割の表示">
        {views.map((v) => <Button key={v.key} variant={view === v.key ? 'secondary' : 'ghost'} aria-pressed={view === v.key} onClick={() => change(v.key)}>{v.label}</Button>)}
      </div>
      {view === 'week' ? <WeekDashboard /> : view === 'calendar' ? <ScheduleCalendar /> : <WebClassTimetable />}
    </div>
  )
}

function WebClassTimetable() {
  const { data, error, isPending, refetch } = usePage<CoursesData>('/courses')
  const { refresh, refreshing } = useRefreshFromWebClass(['/courses'])
  const [todayIdx] = useState(() => new Date().getDay() - 1) // 月=0

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted-foreground">WebClass のコースです。押すと資料と出席確認を見られます。</p>
        <RefreshButton onClick={refresh} refreshing={refreshing} />
      </div>
      {isPending && <LoadingBlock />}
      {error && <ErrorState message={error.message} onRetry={() => refetch()} />}
      {data?.Error && <ErrorState message={data.Error} onRetry={() => refetch()} />}
      {data && !data.Error && !(data.Rows ?? []).length && <EmptyState icon={<CalendarX className="size-5" />}>時間割を取得できませんでした。</EmptyState>}
      {data && (data.Rows ?? []).length > 0 && (
        <>
          {/* パソコン: 表 */}
          <div className="hidden overflow-hidden rounded-xl border bg-card md:block">
            <table className="w-full table-fixed text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="w-14 py-2 text-xs font-normal text-muted-foreground" />
                  {data.Days.map((d, i) => (
                    <th key={d} className={cn('py-2 text-xs font-medium', i === todayIdx ? 'text-primary' : 'text-muted-foreground')}>{d}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {data.Rows.map((row) => (
                  <tr key={row.Period} className="border-b last:border-0">
                    <th className="py-2 text-xs font-normal text-muted-foreground">{row.Period}限</th>
                    {row.Cells.map((cell, i) => (
                      <td key={i} className={cn('h-16 border-l p-1 align-top', i === todayIdx && 'bg-primary/5')}>
                        {(cell ?? []).map((c) => c && (
                          <Link key={c.id} href={`/courses/${c.id}?name=${encodeURIComponent(c.name)}`} className="block rounded-md px-2 py-1.5 text-xs leading-snug hover:bg-muted">
                            {c.name}
                          </Link>
                        ))}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* スマホ: 曜日ごと（今日を先頭に） */}
          <div className="space-y-3 md:hidden">
            {data.Days.map((d, i) => ({ d, i })).sort((a, b) => ((a.i - todayIdx + 7) % 7) - ((b.i - todayIdx + 7) % 7)).map(({ d, i }) => {
              const lessons = data.Rows.flatMap((row) => (row.Cells[i] ?? []).filter(Boolean).map((c) => ({ period: row.Period, c: c! })))
              return (
                <section key={d} className="rounded-xl border bg-card">
                  <h2 className={cn('border-b px-4 py-2 text-sm font-medium', i === todayIdx && 'text-primary')}>{dayNames[d] ?? d}{i === todayIdx && '（今日）'}</h2>
                  {lessons.length === 0 ? (
                    <p className="px-4 py-3 text-sm text-muted-foreground">授業はありません</p>
                  ) : (
                    <ul className="divide-y">
                      {lessons.map(({ period, c }) => (
                        <li key={c.id + period}>
                          <Link href={`/courses/${c.id}?name=${encodeURIComponent(c.name)}`} className="flex min-h-12 items-center gap-3 px-4 py-2 active:bg-muted">
                            <span className="w-8 text-xs tabular-nums text-muted-foreground">{period}限</span>
                            <span className="text-sm">{c.name}</span>
                          </Link>
                        </li>
                      ))}
                    </ul>
                  )}
                </section>
              )
            })}
          </div>
        </>
      )}
    </div>
  )
}
