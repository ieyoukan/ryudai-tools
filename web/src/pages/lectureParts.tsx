import { Users } from 'lucide-react'

import type { LectureAvailability } from '@/api/gen/academic'
import { cn } from '@/lib/utils'

/** 定員・登録者・残席を、残りの割合が見える小さな棒で出す。 */
export function SeatInfo({ a, className }: { a: LectureAvailability; className?: string }) {
  const cap = a.capacity
  const reg = a.registered
  const left = a.seats_left
  return (
    <div className={cn('flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground', className)}>
      {a.registered_by_user && <span className="font-medium text-primary">登録済み</span>}
      <span className="flex items-center gap-1"><Users className="size-3.5" />定員 {cap ?? '不明'}{reg != null && ` ・ 登録 ${reg}`}</span>
      {cap != null && cap > 0 && reg != null && (
        <span className="h-1.5 w-20 overflow-hidden rounded-full bg-track" aria-hidden>
          <span className={cn('block h-full rounded-full', (left ?? 1) <= 0 ? 'bg-critical' : 'bg-primary')} style={{ width: `${Math.min(100, (reg / cap) * 100)}%` }} />
        </span>
      )}
      {left != null && <span className={cn(left <= 0 && 'font-medium text-critical')}>{left <= 0 ? '満員' : `残り ${left}`}</span>}
    </div>
  )
}
