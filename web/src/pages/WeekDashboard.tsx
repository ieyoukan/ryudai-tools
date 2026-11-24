import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CalendarDays, ChevronLeft, ChevronRight, Clock, MapPin, RefreshCw, User } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link } from 'wouter'

import { api } from '@/api/client'
import type { Schedule, ScheduleItem } from '@/api/gen/academic'
import { useMe } from '@/api/page'
import { CampusMap } from '@/components/CampusMap'
import { EmptyState, ErrorState, LoadingBlock } from '@/components/page'
import { Button } from '@/components/ui/button'
import { dayDate, durationLabel, fetchedLabel, lecturePath, mondayOf, shiftDate, shortDate, timeLabel, todayInJapan, weekdayLabels } from '@/lib/schedule'
import { cn } from '@/lib/utils'

/** 30秒ごとに更新する現在時刻（授業中・次の授業の判定用）。 */
function useNow() {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 30_000)
    return () => clearInterval(t)
  }, [])
  return now
}

// 琉大の時限の標準時刻（その週に予定が無い時限の行見出しに使う）
const periodTimes: Record<number, [string, string]> = {
  1: ['08:30', '10:00'], 2: ['10:20', '11:50'], 3: ['12:50', '14:20'], 4: ['14:40', '16:10'],
  5: ['16:20', '17:50'], 6: ['18:00', '19:30'], 7: ['19:40', '21:10'],
}

/** 予定の時限の範囲。「3限」「3・4限」を読み、無ければ開始・終了時刻から決める。 */
function periodRange(e: ScheduleItem): [number, number] {
  const nums = (e.period ?? '').normalize('NFKC').match(/\d+/g)?.map(Number).filter((n) => n >= 1 && n <= 8) ?? []
  if (nums.length) return [Math.min(...nums), Math.max(...nums)]
  const start = timeLabel(e.start), end = timeLabel(e.end)
  const first = Number(Object.entries(periodTimes).filter(([, [s]]) => s <= start).at(-1)?.[0] ?? 1)
  const last = Number(Object.entries(periodTimes).filter(([, [s]]) => s < end).at(-1)?.[0] ?? first)
  return [first, Math.max(first, last)]
}

const trimParens = (s: string) => s.replace(/^[（(]\s*/, '').replace(/\s*[）)]$/, '')

/** 今週の予定。上に「授業中・次の授業」と教室の地図、下に一週間の時間割表。 */
export function WeekDashboard() {
  const today = todayInJapan()
  const [weekStart, setWeekStart] = useState(() => mondayOf(today))
  const { data: me } = useMe()
  const now = useNow()
  const path = `/api/me/schedule?date=${weekStart}&days=7`
  // カレンダー表示（週）と同じキーにして、切り替えても取り直さない。
  const queryKey = ['schedule', me?.Username ?? '', weekStart, 7]
  const qc = useQueryClient()
  const query = useQuery({ queryKey, queryFn: () => api.get<Schedule>(path), enabled: me !== undefined, staleTime: 5 * 60_000, refetchInterval: 5 * 60_000, retry: false })
  const refresh = useMutation({
    mutationFn: () => api.get<Schedule>(path + '&refresh=1'),
    onSuccess: (data) => qc.setQueryData(queryKey, data),
  })
  const data = query.data
  const busy = query.isFetching || refresh.isPending
  const isThisWeek = weekStart === mondayOf(today)
  const events = data?.days.flatMap((d) => d.events) ?? []
  const upcoming = events.filter((e) => !e.cancelled && Date.parse(e.end) > now)
  const current = upcoming.find((e) => Date.parse(e.start) <= now)
  const next = upcoming.find((e) => Date.parse(e.start) > now)
  const focus = isThisWeek ? current ?? next : undefined

  return (
    <section className="space-y-4" aria-label="今週の予定">
      {query.isPending && <LoadingBlock rows={3} label="教務ポータルから今週の予定を取得しています" />}
      {query.error && <ErrorState message={query.error.message} onRetry={() => query.refetch()} />}

      {data && isThisWeek && (
        <div className="grid gap-3 md:grid-cols-5">
          <NowCard current={current} next={next} now={now} today={today} className={focus ? 'md:col-span-2' : 'md:col-span-5'} />
          {focus && <CampusMap location={focus.location} room={focus.room} className="h-60 md:col-span-3 md:h-auto" />}
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon" aria-label="前の週" onClick={() => { setWeekStart(shiftDate(weekStart, -7)); refresh.reset() }}><ChevronLeft /></Button>
          <h2 className="text-center text-sm font-semibold tabular-nums">
            {isThisWeek ? '今週の時間割' : '時間割'}
            <span className="ml-2 font-normal text-muted-foreground">{shortDate(weekStart)} – {shortDate(shiftDate(weekStart, 6))}</span>
          </h2>
          <Button variant="ghost" size="icon" aria-label="次の週" onClick={() => { setWeekStart(shiftDate(weekStart, 7)); refresh.reset() }}><ChevronRight /></Button>
          {!isThisWeek && <Button variant="outline" size="sm" onClick={() => setWeekStart(mondayOf(today))}>今週</Button>}
        </div>
        <Button variant="outline" size="sm" disabled={busy} onClick={() => refresh.mutate()}><RefreshCw className={cn(busy && 'animate-spin')} />最新の情報に更新</Button>
      </div>
      {refresh.error && <ErrorState message={`更新に失敗しました。表示中の予定は更新前の情報です。${refresh.error.message}`} onRetry={() => refresh.mutate()} />}

      {data && (events.length === 0
        ? <EmptyState icon={<CalendarDays className="size-5" />}>この週のポータルに授業の予定はありません。</EmptyState>
        : <WeekGrid data={data} today={today} now={now} />)}

      {data && (
        <p className="text-xs leading-relaxed text-muted-foreground">
          <a href={data.source} target="_blank" rel="noreferrer" className="underline underline-offset-2">教務ポータル</a>の週間時間割から取得（日本時間 · 取得 {fetchedLabel(data.fetched_at)}、最大5分間保存）。講義を押すと、教務の全授業日程と WebClass の資料をタブで切り替えて見られます。
        </p>
      )}
    </section>
  )
}

function NowCard({ current, next, now, today, className }: { current?: ScheduleItem; next?: ScheduleItem; now: number; today: string; className?: string }) {
  const focus = current ?? next
  const minutes = (to: string) => Math.max(0, Math.ceil((Date.parse(to) - now) / 60_000))
  const label = current ? '授業中' : next ? (next.date === today ? '次の授業' : `次の授業 · ${shortDate(next.date)}`) : '今週'
  const remaining = current ? `残り ${durationLabel(minutes(current.end))}` : next?.date === today ? `あと ${durationLabel(minutes(next.start))}` : ''
  const after = current && next && next.date === today ? next : undefined
  const l = focus?.location
  return (
    <div className={cn('flex flex-col rounded-xl border bg-card p-4', current && 'border-primary/40', className)}>
      <div className="flex items-center justify-between gap-2">
        <p className="flex items-center gap-1.5 text-xs font-medium text-primary"><Clock className="size-3.5" />{label}</p>
        {remaining && <span className="text-xs tabular-nums text-muted-foreground">{remaining}</span>}
      </div>
      {focus ? (
        <Link href={focus.lecture_code && focus.year ? lecturePath(focus.year, focus.lecture_code, { date: focus.date, name: focus.title }) : '/courses'} className="mt-2 block flex-1">
          <p className="text-xl font-semibold leading-snug">{focus.title}</p>
          <p className="mt-1 text-sm tabular-nums text-muted-foreground">{timeLabel(focus.start)} – {timeLabel(focus.end)}{focus.period && `（${focus.period}）`}</p>
          {focus.room && (
            <div className="mt-3 flex items-start gap-1.5">
              <MapPin className="mt-0.5 size-4 shrink-0 text-primary" />
              <div>
                <p className="text-base font-semibold">{focus.room}</p>
                {l && !l.online && <p className="text-sm text-muted-foreground">{l.building}{l.detail ? ` ${l.detail}` : l.floor ? ` ${l.floor}階` : ''}</p>}
              </div>
            </div>
          )}
          {focus.instructors && <p className="mt-2 flex items-center gap-1.5 text-sm text-muted-foreground"><User className="size-4" />{trimParens(focus.instructors)}</p>}
        </Link>
      ) : (
        <p className="mt-2 text-sm text-muted-foreground">今週の授業はすべて終わりました。</p>
      )}
      {after && (
        <p className="mt-3 border-t pt-2 text-xs text-muted-foreground">
          その次: <span className="text-foreground">{after.title}</span> <span className="tabular-nums">{timeLabel(after.start)}</span>{after.room && <> · <span className="text-foreground">{after.room}</span></>}
        </p>
      )}
    </div>
  )
}

type Placed = { e: ScheduleItem; col: number; first: number; last: number }

/** 一週間の時間割表（行: 時限、列: 曜日）。同じコマに複数の予定があれば縦に並べる。 */
function WeekGrid({ data, today, now }: { data: Schedule; today: string; now: number }) {
  // 平日は常に、土日は予定があるときだけ列を出す。
  const days = data.days.filter((d, i) => i < 5 || d.events.length > 0)
  const placed: Placed[] = days.flatMap((d, col) => d.events.map((e) => { const [first, last] = periodRange(e); return { e, col, first, last } }))
  const minP = Math.min(1, ...placed.map((p) => p.first))
  const maxP = Math.max(5, ...placed.map((p) => p.last))
  const periods = Array.from({ length: maxP - minP + 1 }, (_, i) => minP + i)
  // 同じ列・同じ開始時限の予定をまとめる
  const slots = new Map<string, Placed[]>()
  for (const p of placed) {
    const k = `${p.col}:${p.first}`
    slots.set(k, [...(slots.get(k) ?? []), p])
  }

  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      <div className="grid" style={{ gridTemplateColumns: `minmax(2rem, auto) repeat(${days.length}, minmax(0, 1fr))`, gridTemplateRows: `auto repeat(${periods.length}, minmax(5.5rem, auto))` }}>
        {/* 見出し行 */}
        <div className="border-b bg-muted/40" style={{ gridRow: 1, gridColumn: 1 }} />
        {days.map((d, i) => {
          const isToday = d.date === today
          return (
            <div key={d.date} className={cn('flex flex-col items-center gap-0.5 border-b border-l bg-muted/40 py-2 text-xs', isToday && 'bg-primary/10')} style={{ gridRow: 1, gridColumn: i + 2 }}>
              <span className={cn('font-medium', isToday ? 'text-primary' : 'text-muted-foreground')}>{weekdayLabels[(dayDate(d.date).getUTCDay() + 6) % 7]}</span>
              <span className={cn('inline-flex h-5 items-center justify-center rounded-full px-1.5 text-[11px] tabular-nums', isToday ? 'bg-primary font-semibold text-primary-foreground' : 'text-muted-foreground')}>{Number(d.date.slice(5, 7))}/{Number(d.date.slice(8))}</span>
            </div>
          )
        })}
        {/* 時限の見出しと空きコマの枠 */}
        {periods.map((n, r) => {
          const last = r === periods.length - 1
          return [
            <div key={`h${n}`} className={cn('flex flex-col items-center justify-center px-1 text-center', !last && 'border-b')} style={{ gridRow: r + 2, gridColumn: 1 }}>
              <span className="text-sm font-semibold tabular-nums">{n}</span>
              {periodTimes[n] && <span className="hidden text-[10px] leading-tight tabular-nums text-muted-foreground sm:block">{periodTimes[n][0]}<br />{periodTimes[n][1]}</span>}
            </div>,
            ...days.map((d, i) => <div key={`c${n}-${d.date}`} className={cn('border-l', !last && 'border-b', d.date === today && 'bg-primary/[0.04]')} style={{ gridRow: r + 2, gridColumn: i + 2 }} />),
          ]
        })}
        {/* 予定 */}
        {[...slots.values()].map((group) => {
          const { col, first } = group[0]
          const last = Math.max(...group.map((p) => p.last))
          return (
            <div key={`${col}:${first}`} className="flex flex-col gap-1 p-0.5 sm:p-1" style={{ gridColumn: col + 2, gridRow: `${first - minP + 2} / span ${last - first + 1}` }}>
              {group.map(({ e }) => <GridEvent key={e.id} e={e} now={now} />)}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function GridEvent({ e, now }: { e: ScheduleItem; now: number }) {
  const ended = Date.parse(e.end) <= now
  const live = !e.cancelled && Date.parse(e.start) <= now && now < Date.parse(e.end)
  const badge = live ? '授業中' : e.cancelled ? e.status || '休講' : e.status
  const body = (
    <>
      {badge && <span className={cn('mb-0.5 inline-block rounded px-1 text-[10px] font-medium leading-4', live ? 'bg-primary text-primary-foreground' : 'bg-critical/15 text-critical')}>{badge}</span>}
      <p className={cn('line-clamp-3 text-[11px] font-medium leading-snug sm:text-xs', e.cancelled && 'line-through')}>{e.title}</p>
      <p className="mt-0.5 hidden text-[11px] tabular-nums text-muted-foreground sm:block">{timeLabel(e.start)}–{timeLabel(e.end)}</p>
      {e.room && <p className="mt-0.5 flex items-start gap-0.5 text-[10px] leading-tight text-muted-foreground sm:text-[11px]"><MapPin className="mt-px hidden size-3 shrink-0 sm:block" /><span className="break-all">{e.room}</span></p>}
    </>
  )
  const cls = cn(
    'block flex-1 rounded-md border-l-[3px] border-primary bg-primary/10 px-1 py-1 text-left transition-colors sm:rounded-lg sm:px-2 sm:py-1.5',
    e.lecture_code && 'hover:bg-primary/15 active:bg-primary/20',
    ended && !live && 'opacity-55',
    e.cancelled && 'border-critical bg-muted',
    live && 'ring-2 ring-primary',
  )
  return e.lecture_code && e.year
    ? <Link href={lecturePath(e.year, e.lecture_code, { date: e.date, name: e.title })} className={cls} aria-label={`${e.title}、${timeLabel(e.start)}から、${e.room ?? '教室未定'}`}>{body}</Link>
    : <div className={cls}>{body}</div>
}
