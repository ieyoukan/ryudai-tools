import { useEffect, useState } from 'react'

import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

const steps = ['成績', '個別成績表', '今期の履修登録', '集計']

/** 初回は教務システムからの読み込みに 10〜20 秒かかるので、どこまで進んだかを段階で見せる。 */
export function PlanLoading() {
  const [sec, setSec] = useState(0)
  useEffect(() => {
    const started = Date.now()
    const t = setInterval(() => setSec(Math.floor((Date.now() - started) / 1000)), 500)
    return () => clearInterval(t)
  }, [])
  const at = Math.min(Math.floor(sec / 4), steps.length - 1)
  return (
    <div role="status" aria-live="polite" className="space-y-4">
      <Card>
        <CardContent className="space-y-3">
          <div className="flex items-baseline justify-between">
            <p className="text-sm font-medium">教務情報システムから読み込み中</p>
            <p className="text-xs tabular-nums text-muted-foreground">{sec} 秒</p>
          </div>
          <ol className="grid grid-cols-4 gap-1.5">
            {steps.map((s, i) => (
              <li key={s} className="space-y-1">
                <div className={`h-1 rounded-full ${i < at ? 'bg-primary' : i === at ? 'animate-pulse bg-primary/50' : 'bg-track'}`} />
                <p className={`text-[11px] ${i <= at ? 'text-foreground' : 'text-muted-foreground'}`}>{s}</p>
              </li>
            ))}
          </ol>
          <p className="text-xs text-muted-foreground">初回は 10〜20 秒ほどかかります</p>
        </CardContent>
      </Card>
      <Skeleton className="h-40 rounded-xl" />
      <div className="grid gap-4 md:grid-cols-2">
        <Skeleton className="h-64 rounded-xl" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    </div>
  )
}
