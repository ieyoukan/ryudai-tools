import { AlertTriangle, CircleCheck, CircleHelp, TrendingUp } from 'lucide-react'
import { Link } from 'wouter'

import type { GraduationStatus } from '@/api/gen/academic'
import { credits } from '@/lib/format'
import { cn } from '@/lib/utils'

/** 卒業要件を満たせるかの判定（成績ページだけに出す）。色は状態にだけ使い、必ずアイコンと文を添える。 */
export function Verdict({ status: st }: { status: GraduationStatus }) {
  const o = st.outlook
  if (!o) return null
  const byPlan = o.basis === 'plan'
  const view = {
    met: { icon: CircleCheck, tone: 'text-good', title: '卒業に必要な単位を満たしている見込みです', sub: '必修科目やその他の条件を含め、最終的な卒業判定は大学が行います。' },
    on_track: {
      icon: TrendingUp,
      tone: 'text-primary',
      title: `まだ満たしていませんが、${byPlan ? '計画どおりなら' : 'このままのペースなら'}満たせる見込みです`,
      sub: `卒業まであと ${credits(st.total_shortfall)} 単位${o.expected_graduation ? ` ・ 卒業予定 ${o.expected_graduation}` : ''}`,
    },
    at_risk: {
      icon: AlertTriangle,
      tone: 'text-critical',
      title: 'このままでは卒業要件を満たせません',
      sub: `${o.expected_graduation ? `卒業予定（${o.expected_graduation}）までに、` : ''}${byPlan ? '今の履修計画どおりに単位を取っても' : '今のペースでは'}卒業に必要な単位を満たせない見込みです。`,
    },
    unknown: { icon: CircleHelp, tone: 'text-muted-foreground', title: '卒業要件を判定できませんでした', sub: o.reasons.join(' ') || '個別成績表から必要単位を読み取れませんでした。' },
  }[o.status as 'met' | 'on_track' | 'at_risk' | 'unknown'] ?? null
  if (!view) return null
  const Icon = view.icon
  return (
    <section className={cn('rounded-xl border bg-card p-4', o.status === 'at_risk' && 'border-critical/50')} aria-label="卒業要件の判定">
      <div className="flex gap-3">
        <Icon className={cn('mt-0.5 size-5 shrink-0', view.tone)} />
        <div className="min-w-0 space-y-1">
          <p className="font-semibold">{view.title}</p>
          <p className="text-sm text-muted-foreground">{view.sub}</p>
          {o.status === 'at_risk' && o.reasons.length > 0 && (
            <ul className="list-disc space-y-0.5 pl-5 text-sm text-muted-foreground">{o.reasons.map((r, i) => <li key={i}>{r}</li>)}</ul>
          )}
          {(o.status === 'at_risk' || o.status === 'on_track') && (
            <Link href="/plan" className="inline-block pt-1 text-sm text-primary underline-offset-4 hover:underline">卒業までの計画を{byPlan ? '見直す' : '立てる'}</Link>
          )}
        </div>
      </div>
    </section>
  )
}
