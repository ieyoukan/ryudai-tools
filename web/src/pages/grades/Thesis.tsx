import { CircleCheck, CircleX } from 'lucide-react'

import type { ThesisCheck } from '@/api/gen/academic'
import { SectionTitle } from '@/components/page'
import { Card, CardContent } from '@/components/ui/card'
import { credits } from '@/lib/format'
import { cn } from '@/lib/utils'

/** 3 年次の、卒業研究（研究室仮配属）の登録条件の見込み。条件ごとに、届いているかを棒と印で見せる。 */
export function Thesis({ t }: { t?: ThesisCheck }) {
  if (!t) return null
  return (
    <Card>
      <CardContent className="space-y-3">
        <SectionTitle>研究室仮配属・卒業研究の登録条件</SectionTitle>
        {t.status === 'likely' && <p className="flex items-center gap-2 text-sm"><CircleCheck className="size-4 text-good" />{t.check_term}（4 年次の 4 月）に卒業研究を登録できる見込みです</p>}
        {t.status === 'at_risk' && <p className="flex items-center gap-2 text-sm"><CircleX className="size-4 text-critical" />このままでは {t.check_term}（4 年次の 4 月）に卒業研究を登録できない見込みです</p>}
        {t.conditions.length > 0 && (
          <ul className="divide-y rounded-lg border">
            {t.conditions.map((c, i) => (
              <li key={i} className="flex gap-3 px-3 py-2.5">
                {c.ok ? <CircleCheck className="mt-0.5 size-4 shrink-0 text-good" /> : <CircleX className="mt-0.5 size-4 shrink-0 text-critical" />}
                <div className="min-w-0 flex-1 space-y-1">
                  <p className="text-sm font-medium">{c.label}</p>
                  {c.kind === 'credits_min' && (
                    <>
                      <p className="text-xs text-muted-foreground">{t.check_term} 時点の見込み {credits(c.value)} / {credits(c.target)} 単位{!c.ok && `（あと ${credits(c.target - c.value)}）`}</p>
                      <div className="h-2 overflow-hidden rounded-[4px] bg-track">
                        <div className={cn('h-full', c.ok ? 'bg-primary' : 'bg-primary/60')} style={{ width: `${Math.min(100, (c.value / Math.max(c.target, 1)) * 100)}%` }} />
                      </div>
                    </>
                  )}
                  {c.kind === 'remaining_max' && <p className="text-xs text-muted-foreground">{t.check_term} 時点の見込み 残り {credits(c.value)} 単位（{credits(c.target)} 単位以下が条件）</p>}
                  {c.kind === 'missing_required_max' && <p className="text-xs text-muted-foreground">未修得の見込み {credits(c.value)} 科目</p>}
                  {(c.items ?? []).length > 0 && <p className="text-xs text-critical">{c.kind === 'courses' ? '' : '未修得: '}{c.items!.join('、')}</p>}
                </div>
              </li>
            ))}
          </ul>
        )}
        {t.rule && (
          <blockquote className="border-l-2 pl-3 text-xs text-muted-foreground">
            {t.rule.evidence.text}
            <cite className="mt-1 block not-italic"><a href={`${t.rule.evidence.source.url}#page=${t.rule.evidence.page}`} target="_blank" rel="noopener" className="underline">{t.rule.evidence.document} p.{t.rule.evidence.page}（学生便覧）</a></cite>
          </blockquote>
        )}
        {t.notes.map((n, i) => <p key={i} className="text-xs text-muted-foreground">{n}</p>)}
        {!t.mentioned && t.document_url && <a href={t.document_url} target="_blank" rel="noopener" className="text-xs underline">コースの便覧を開く</a>}
      </CardContent>
    </Card>
  )
}
