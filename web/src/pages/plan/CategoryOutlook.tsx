import { AlertTriangle, CircleCheck } from 'lucide-react'

import type { GraduationStatus } from '@/api/gen/academic'
import { FillLegend, StackedBar } from '@/components/charts'
import type { CategoryColors } from '@/lib/categories'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { credits } from '@/lib/format'

/** 区分ごとに、修得・今期の登録・計画を積み上げて、必須単位（棒の右端）に届くかを見せる。足りない区分を上に出す。 */
export function CategoryOutlook({ status, colors }: { status: GraduationStatus; colors: CategoryColors }) {
  const cats = [...(status.plan?.categories ?? [])].sort((a, b) => b.shortfall_after_plan - a.shortfall_after_plan)
  if (cats.length === 0) return null
  const short = cats.filter((c) => c.shortfall_after_plan > 0).length
  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
        <CardTitle>区分ごとの必須単位</CardTitle>
        <FillLegend />
      </CardHeader>
      <CardContent>
        <p className="mb-3 text-xs text-muted-foreground">
          {short > 0 ? `${cats.length} 区分のうち ${short} 区分が、計画どおりでも足りません` : `${cats.length} 区分すべて、計画どおりなら届きます`}
        </p>
        <ul className="space-y-3">
          {cats.map((c) => (
            <li key={c.section + c.name} className="space-y-1.5">
              <div className="flex items-baseline justify-between gap-2 text-sm">
                <span className="flex min-w-0 items-center gap-1.5 truncate">
                  <span className="size-2.5 shrink-0 rounded-[3px] fill-earned" style={{ ['--c' as string]: colors.color(c.name) }} aria-hidden />
                  {c.name}
                  <span className="ml-1.5 text-xs text-muted-foreground">{c.section}</span>
                </span>
                <span className="flex shrink-0 items-center gap-2 text-xs">
                  <span className="tabular-nums text-muted-foreground">{credits(c.earned + c.registered + c.planned)}/{credits(c.required)}</span>
                  {c.shortfall_after_plan > 0 ? (
                    <span className="flex items-center gap-1 font-medium"><AlertTriangle className="size-3.5 text-warning" />あと {credits(c.shortfall_after_plan)}</span>
                  ) : (
                    <span className="flex items-center gap-1 text-muted-foreground"><CircleCheck className="size-3.5 text-good" />届く</span>
                  )}
                </span>
              </div>
              <StackedBar
                max={c.required}
                segments={(['earned', 'registered', 'planned'] as const).map((f) => ({ key: c.name, color: colors.color(c.name), fill: f, value: c[f], label: c.name }))}
                label={`${c.name}: 必須 ${credits(c.required)}、修得 ${credits(c.earned)}、今期 ${credits(c.registered)}、計画 ${credits(c.planned)}`}
              />
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}
