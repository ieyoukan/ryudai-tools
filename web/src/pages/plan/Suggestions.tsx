import { Check, Plus } from 'lucide-react'

import { useAddPlanItem, useSuggestions } from '@/api/hooks'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { credits } from '@/lib/format'

/** その学期に足すと、計画後も足りない区分を埋められそうな科目（シラバスから）。 */
export function Suggestions({ term }: { term: string }) {
  const { data, error, isPending } = useSuggestions(term, true)
  const add = useAddPlanItem()

  if (isPending) {
    return (
      <div className="space-y-2" role="status" aria-label="シラバスから候補を探しています">
        <p className="text-xs text-muted-foreground">シラバスから探しています…</p>
        {[0, 1, 2].map((i) => <Skeleton key={i} className="h-12 rounded-md" />)}
      </div>
    )
  }
  if (error) return <p className="rounded-md bg-muted p-3 text-sm">{error.message}</p>

  return (
    <div className="space-y-4">
      {data.groups.length === 0 && <p className="rounded-md bg-muted p-3 text-sm text-muted-foreground">足りない区分に入りそうな科目は見つかりませんでした</p>}
      {data.groups.map((g) => (
        <section key={g.section + g.name} className="space-y-1">
          <div className="flex items-baseline justify-between gap-2">
            <h4 className="text-sm font-medium">{g.name}</h4>
            <span className="text-xs text-muted-foreground">あと <span className="font-medium text-foreground tabular-nums">{credits(g.shortfall)}</span> 単位</span>
          </div>
          <ul className="divide-y rounded-md border">
            {g.items.map((s) => {
              const adding = add.isPending && add.variables?.title === s.title
              const added = add.isSuccess && add.variables?.title === s.title && add.variables.year_term === term
              return (
                <li key={s.lecture_code} className="flex min-h-12 items-center gap-2 px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <a href={`/syllabus/${data.syllabus_year}/${s.lecture_code}`} target="_blank" rel="noopener" className="truncate text-sm hover:underline">{s.title}</a>
                      {s.required && <Badge variant="outline" className="shrink-0">必修</Badge>}
                      {s.retake && <Badge variant="outline" className="shrink-0">再履修</Badge>}
                    </div>
                    <p className="truncate text-xs text-muted-foreground" title={s.basis}>
                      {credits(s.credits)}単位{s.day_period && ` ・ ${s.day_period}`}{!s.retake && ' ・ 区分は推定'}
                    </p>
                  </div>
                  <Button
                    size="icon"
                    variant={added ? 'secondary' : 'outline'}
                    className="size-9 shrink-0"
                    disabled={adding || added}
                    onClick={() => add.mutate({ year_term: term, title: s.title, section: s.section, category: s.category, credits: s.credits })}
                    aria-label={added ? `${s.title} は追加済み` : `${s.title} を ${term} の計画に追加`}
                  >
                    {added ? <Check /> : <Plus />}
                  </Button>
                </li>
              )
            })}
          </ul>
        </section>
      ))}
      <p className="text-xs text-muted-foreground">
        {data.syllabus_year}年度 {data.syllabus_term}のシラバスから。区分は成績から推定しています。{data.notes.length > 0 && <details className="mt-1 inline"><summary className="inline cursor-pointer underline-offset-4 hover:underline">詳しく</summary>{data.notes.map((n, i) => <span key={i} className="mt-1 block">{n}</span>)}</details>}
      </p>
    </div>
  )
}
