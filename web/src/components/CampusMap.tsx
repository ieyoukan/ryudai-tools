import { Building2, ExternalLink, Footprints, MapPinOff, Wifi } from 'lucide-react'

import type { Location } from '@/api/gen/academic'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

/** 「2階」「1階（共創技術ラボ）」 */
function floorLabel(l: Location) {
  if (l.detail) return l.detail
  return l.floor ? `${l.floor}階` : ''
}

/**
 * 教室の建物を Google マップで表示する。API キー不要の埋め込み地図に、建物名・部屋・階を重ねる。
 * 建物を特定できない教室は、地図を出さずにその旨を出す（違う建物を指さないため）。
 */
export function CampusMap({ location: l, room, className }: { location?: Location; room?: string; className?: string }) {
  if (l?.online) {
    return (
      <div className={cn('flex flex-col items-center justify-center gap-2 rounded-xl border bg-card p-6 text-center', className)}>
        <Wifi className="size-6 text-primary" />
        <p className="text-sm font-medium">オンライン授業</p>
        <p className="text-xs text-muted-foreground">教室は「{room}」です。</p>
      </div>
    )
  }
  if (!l) {
    return (
      <div className={cn('flex flex-col items-center justify-center gap-2 rounded-xl border bg-card p-6 text-center', className)}>
        <MapPinOff className="size-6 text-muted-foreground" />
        <p className="text-sm font-medium">{room ? `「${room}」の建物は地図に未登録です` : '教室が登録されていません'}</p>
        {room && (
          <Button asChild variant="outline" size="sm">
            <a href={`https://www.google.com/maps/search/?api=1&query=${encodeURIComponent('琉球大学 ' + room)}`} target="_blank" rel="noreferrer"><ExternalLink />Google マップで探す</a>
          </Button>
        )}
      </div>
    )
  }
  const hasCoords = !!l.lat && !!l.lng
  const src = `https://maps.google.com/maps?q=${encodeURIComponent(l.map_query)}&z=${hasCoords ? 18 : 15}&hl=ja&output=embed`
  const directions = `https://www.google.com/maps/dir/?api=1&destination=${encodeURIComponent(l.map_query)}&travelmode=walking`
  const floor = floorLabel(l)
  return (
    <div className={cn('relative isolate min-h-56 overflow-hidden rounded-xl border bg-muted', className)}>
      <iframe
        key={src}
        title={`${l.building}の地図`}
        src={src}
        className="absolute inset-0 size-full border-0 dark:hue-rotate-180 dark:invert-[0.9]"
        loading="lazy"
        referrerPolicy="no-referrer-when-downgrade"
      />
      {/* 地図の上に重ねる建物ラベルと操作（下端は Google の表示と重なるので上に置く） */}
      <div className="pointer-events-none absolute inset-x-0 top-0 flex flex-wrap items-start justify-between gap-2 bg-gradient-to-b from-black/40 to-transparent p-3 pb-10">
        <div className="pointer-events-auto inline-flex max-w-full items-center gap-2 rounded-lg bg-background/95 px-3 py-2 shadow-sm ring-1 ring-black/5 backdrop-blur">
          <Building2 className="size-4 shrink-0 text-primary" />
          <div className="min-w-0 leading-tight">
            <p className="truncate text-sm font-semibold">{l.building}</p>
            <p className="truncate text-xs text-muted-foreground">
              {[l.room && `${l.room}室`, floor, l.campus].filter(Boolean).join(' · ')}
            </p>
          </div>
        </div>
        <div className="pointer-events-auto flex gap-1.5">
          <Button asChild size="sm" className="shadow-md"><a href={directions} target="_blank" rel="noreferrer"><Footprints />経路</a></Button>
          <Button asChild size="sm" variant="outline" className="bg-background/95 shadow-md backdrop-blur"><a href={l.map_url} target="_blank" rel="noreferrer"><ExternalLink />マップ</a></Button>
        </div>
      </div>
    </div>
  )
}
