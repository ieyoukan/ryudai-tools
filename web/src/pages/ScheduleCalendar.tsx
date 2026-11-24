import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CalendarDays, CalendarPlus, ChevronLeft, ChevronRight, Download, MapPin, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'wouter'

import { api } from '@/api/client'
import type { Schedule, ScheduleItem } from '@/api/gen/academic'
import { useMe } from '@/api/page'
import { EmptyState, ErrorState, LoadingBlock } from '@/components/page'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { dateKey, dayDate, fetchedLabel, fullDate, lecturePath, mondayOf, shiftDate, timeLabel, todayInJapan, validDate, weekdayLabels } from '@/lib/schedule'
import { cn } from '@/lib/utils'

const shift = shiftDate
const monday = mondayOf
const weekdays = weekdayLabels

export function ScheduleCalendar() {
  const [date, setDate] = useState(() => {
    const initial = new URLSearchParams(location.search).get('date') ?? ''
    return validDate(initial) ? initial : todayInJapan()
  })
  const { data: me } = useMe()
  const today = todayInJapan()
  const monthStart = date.slice(0, 7) + '-01'
  const monthEnd = new Date(Date.UTC(dayDate(date).getUTCFullYear(), dayDate(date).getUTCMonth() + 1, 0, 12))
  const start = monthStart
  const days = monthEnd.getUTCDate()
  const params = new URLSearchParams({ date: start, days: String(days) })
  const path = '/api/me/schedule?' + params
  const queryKey = ['schedule', me?.Username ?? '', start, days]
  const qc = useQueryClient()
  const query = useQuery({ queryKey, queryFn: () => api.get<Schedule>(path), enabled: me !== undefined, staleTime: 5 * 60_000, refetchInterval: 5 * 60_000, retry: false })
  const refresh = useMutation({
    mutationFn: (request: { path: string; queryKey: (string | number)[] }) => api.get<Schedule>(request.path + '&refresh=1'),
    onSuccess: (data, request) => {
      qc.removeQueries({ queryKey: ['schedule'], type: 'inactive' })
      qc.setQueryData(request.queryKey, data)
    },
  })
  const data = query.data
  const busy = query.isFetching || refresh.isPending
  const selected = data?.days.find((d) => d.date === date)
  const gridStart = monday(monthStart)
  const gridLength = Math.ceil(((dayDate(monthStart).getUTCDay() + 6) % 7 + days) / 7) * 7
  const grid = Array.from({ length: gridLength }, (_, i) => shift(gridStart, i))
  const changeDate = (next: string) => { if (validDate(next)) { setDate(next); refresh.reset() } }
  const move = (direction: number) => {
    const d = dayDate(monthStart); d.setUTCMonth(d.getUTCMonth() + direction); changeDate(dateKey(d))
  }
  const download = async () => {
    const response = await fetch(path + '&format=ics', { credentials: 'same-origin' })
    if (!response.ok) {
      const body = await response.json().catch(() => null) as { error?: string } | null
      throw new Error(body?.error ?? 'カレンダーファイルを取得できませんでした')
    }
    const url = URL.createObjectURL(await response.blob())
    const a = document.createElement('a'); a.href = url; a.download = `university-${start}.ics`; a.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  const exportCalendar = useMutation({ mutationFn: download })

  return (
    <section className="space-y-4" aria-label="大学の予定">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="flex items-center gap-2 text-lg font-semibold"><CalendarDays className="size-5 text-primary" />大学の予定</h2>
          <p className="mt-1 text-xs text-muted-foreground">教務ポータルの日付ごとの時間割です。休日・大学行事による授業日の変更も反映します。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" disabled={busy} onClick={() => refresh.mutate({ path, queryKey })}><RefreshCw className={cn(busy && 'animate-spin')} />最新の情報に更新</Button>
          <Button variant="outline" size="sm" disabled={!data || busy || exportCalendar.isPending} onClick={() => exportCalendar.mutate()}><Download />この月の予定を一括保存 (.ics)</Button>
        </div>
      </div>
      <div className="rounded-xl border bg-card">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b p-3">
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="icon" aria-label="前の月" onClick={() => move(-1)}><ChevronLeft /></Button>
            <h3 className="min-w-28 text-center text-sm font-semibold tabular-nums">{date.slice(0, 4)}年{Number(date.slice(5, 7))}月</h3>
            <Button variant="ghost" size="icon" aria-label="次の月" onClick={() => move(1)}><ChevronRight /></Button>
            <Button variant="outline" size="sm" onClick={() => changeDate(today)}>今日</Button>
          </div>
          <div className="flex items-center gap-2">
            <Input className="w-auto text-sm" aria-label="表示する日付" type="date" min="2000-01-01" max="2100-12-31" value={date} onChange={(e) => changeDate(e.target.value)} />
          </div>
        </div>
        <div className="grid grid-cols-7 border-b bg-muted/30 text-center text-xs text-muted-foreground">
          {weekdays.map((d) => <div key={d} className="py-2">{d}</div>)}
        </div>
        <div className="grid grid-cols-7">
          {grid.map((key, i) => {
            const day = data?.days.find((d) => d.date === key)
            const inRange = key >= start && key <= shift(start, days - 1)
            return (
              <button key={key} type="button" aria-label={`${fullDate(key)}${day ? `、${day.events.length}件の予定` : '、未取得'}`} aria-pressed={key === date} onClick={() => changeDate(key)} className={cn('flex min-w-0 flex-col items-stretch justify-start border-b p-1.5 text-left transition-colors hover:bg-muted/60 focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-primary sm:p-2', 'min-h-16 sm:min-h-28', i % 7 !== 6 && 'border-r', !inRange && 'bg-muted/30 text-muted-foreground', key === date && 'bg-primary/5 ring-2 ring-inset ring-primary/40')}>
                <span className={cn('inline-flex size-6 items-center justify-center rounded-full text-xs tabular-nums', key === today && 'bg-primary font-semibold text-primary-foreground', key === date && key !== today && 'font-bold text-primary')}>{Number(key.slice(8))}</span>
                {inRange && <>
                  <div className="mt-1 hidden space-y-1 sm:block">
                    {day?.events.slice(0, 3).map((e) => <div key={e.id} className={cn('rounded border-l-2 border-primary/60 bg-primary/5 px-1 py-0.5 text-[11px] leading-snug', e.cancelled && 'border-muted-foreground text-muted-foreground line-through')}><span className="tabular-nums text-muted-foreground">{timeLabel(e.start)}</span><span className="ml-1 line-clamp-2">{e.title}</span></div>)}
                    {day && day.events.length > (3) && <p className="text-[10px] text-muted-foreground">ほか{day.events.length - (3)}件</p>}
                    {day && day.events.length === 0 && <p className="text-[10px] text-muted-foreground">予定なし</p>}
                    {!day && <p className="text-[10px] text-muted-foreground">{query.isPending ? '取得中…' : '未取得'}</p>}
                  </div>
                  <div className="mt-1 flex justify-center gap-0.5 sm:hidden" aria-hidden="true">{day ? day.events.slice(0, 4).map((e) => <span key={e.id} className={cn('size-1 rounded-full bg-primary', e.cancelled && 'bg-muted-foreground')} />) : <span className="text-[10px] text-muted-foreground">…</span>}</div>
                </>}
              </button>
            )
          })}
        </div>
      </div>
      {query.isPending && <LoadingBlock rows={2} label="月の予定を取得しています" />}
      {query.error && <ErrorState message={query.error.message} onRetry={() => query.refetch()} />}
      {refresh.error && <ErrorState message={`更新に失敗しました。表示中の予定は更新前の情報です。${refresh.error.message}`} onRetry={() => refresh.mutate({ path, queryKey })} />}
      {exportCalendar.error && <ErrorState message={exportCalendar.error.message} onRetry={() => exportCalendar.mutate()} />}
      {selected && <div className="space-y-3" aria-live="polite">
        <h3 className="text-sm font-semibold">{fullDate(date)}{date === today && '（今日）'} <span className="ml-1 font-normal text-muted-foreground">{selected.events.length}件</span></h3>
        {selected.events.length ? selected.events.map((event) => <EventCard key={event.id} event={event} />) : <EmptyState icon={<CalendarDays className="size-5" />}>この日のポータルに予定はありません。</EmptyState>}
      </div>}
      {data && <p className="text-xs leading-relaxed text-muted-foreground"><a href={data.source} target="_blank" rel="noreferrer" className="underline underline-offset-2">教務ポータル</a> · 日本時間 · 取得 {fetchedLabel(data.fetched_at)}（最大5分間保存）<br />Google カレンダーへの追加は各予定のボタンから。月の予定をまとめて追加する場合は「この月の予定を一括保存 (.ics)」で保存し、パソコン版 Google カレンダーの「設定 → インポート / エクスポート」で読み込みます。追加後の予定は自動更新されません。</p>}
    </section>
  )
}

function EventCard({ event: e }: { event: ScheduleItem }) {
  return (
    <article className={cn('flex flex-wrap items-start justify-between gap-3 rounded-xl border bg-card p-4', e.cancelled && 'bg-muted/40')}>
      <div className="min-w-0 space-y-1">
        <p className="text-xs tabular-nums text-muted-foreground">{timeLabel(e.start)} – {timeLabel(e.end)}{e.period && ` · ${e.period}`}</p>
        <h4 className={cn('text-sm font-medium', e.cancelled && 'text-muted-foreground line-through')}>{e.title}</h4>
        {e.room && <p className="flex items-center gap-1 text-xs text-muted-foreground"><MapPin className="size-3" />{e.room}{e.location && !e.location.online && <span>（{e.location.building}）</span>}</p>}
        {e.instructors && <p className="text-xs text-muted-foreground">{e.instructors}</p>}
        {[e.status, e.theme, e.note].filter(Boolean).map((s, i) => <p key={i} className={cn('text-xs', e.cancelled ? 'text-critical' : 'text-muted-foreground')}>{s}</p>)}
      </div>
      <div className="flex flex-wrap gap-2">
        {e.lecture_code && e.year && <Button asChild variant="ghost" size="sm"><Link href={lecturePath(e.year, e.lecture_code, { date: e.date, name: e.title })}>授業日程・資料</Link></Button>}
        {e.google_calendar_url && <Button asChild variant="outline" size="sm"><a href={e.google_calendar_url} target="_blank" rel="noreferrer"><CalendarPlus />Google カレンダーに追加</a></Button>}
      </div>
    </article>
  )
}
