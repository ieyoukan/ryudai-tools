// 予定の日付は UTC の正午で計算し、表示は日本時間に固定する。端末のタイムゾーンに依存しない。

export const dayDate = (date: string) => new Date(date + 'T12:00:00Z')
export const dateKey = (d: Date) => d.toISOString().slice(0, 10)
export const shiftDate = (date: string, days: number) => { const d = dayDate(date); d.setUTCDate(d.getUTCDate() + days); return dateKey(d) }
export const mondayOf = (date: string) => shiftDate(date, -((dayDate(date).getUTCDay() + 6) % 7))
export const todayInJapan = () => new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Tokyo' }).format(new Date())
/** 日本時間の今の時刻（HH:MM）。 */
export const nowClockInJapan = () => new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Tokyo', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(new Date())
export const validDate = (s: string) => /^\d{4}-\d{2}-\d{2}$/.test(s) && !Number.isNaN(dayDate(s).getTime()) && dateKey(dayDate(s)) === s && s >= '2000-01-01' && s <= '2100-12-31'
/** ISO 日時を日本時間の HH:MM にする。 */
export const timeLabel = (s: string) => new Intl.DateTimeFormat('ja-JP', { timeZone: 'Asia/Tokyo', hour: '2-digit', minute: '2-digit' }).format(new Date(s))
export const fetchedLabel = (s: string) => new Intl.DateTimeFormat('ja-JP', { timeZone: 'Asia/Tokyo', month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(s))
/** 11月4日水曜日 */
export const fullDate = (s: string) => new Intl.DateTimeFormat('ja-JP', { timeZone: 'UTC', month: 'long', day: 'numeric', weekday: 'long' }).format(dayDate(s))
/** 11/4（水） */
export const shortDate = (s: string) => `${Number(s.slice(5, 7))}/${Number(s.slice(8))}（${'日月火水木金土'[dayDate(s).getUTCDay()]}）`
export const weekdayLabels = ['月', '火', '水', '木', '金', '土', '日']

/** 「1時間5分」「25分」 */
export function durationLabel(minutes: number) {
  const h = Math.floor(minutes / 60), m = minutes % 60
  return h ? `${h}時間${m ? `${m}分` : ''}` : `${m}分`
}

/** HH:MM 同士の差（分）。 */
export const minutesBetween = (from: string, to: string) => (Number(to.slice(0, 2)) * 60 + Number(to.slice(3, 5))) - (Number(from.slice(0, 2)) * 60 + Number(from.slice(3, 5)))

/** 講義ページへのリンク。date を付けるとその日の回を強調する。 */
export const lecturePath = (year: number, code: string, opts: { date?: string; name?: string } = {}) => {
  const q = new URLSearchParams()
  if (opts.date) q.set('date', opts.date)
  if (opts.name) q.set('name', opts.name)
  const qs = q.toString()
  return `/lectures/${year}/${encodeURIComponent(code)}${qs ? '?' + qs : ''}`
}

/** WebClass のコース ID の慣例（年度下2桁 + S + 講義コード）。 */
export const webClassCourseID = (year: number, code: string) => `${String(year % 100).padStart(2, '0')}S${code}`
