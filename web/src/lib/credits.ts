import type { GraduationStatus, PlanYearTerm } from '@/api/gen/academic'
import type { Fill, Segment } from '@/components/charts'
import { OTHER, categoryColors, requirementOf, type CategoryColors } from '@/lib/categories'

// 単位を区分ごと・塗り方（修得・今期の登録・計画）ごとに集めて、グラフの部品（Segment）にする。

type Totals = Map<string, Record<Fill, number>>

function add(t: Totals, name: string, fill: Fill, v: number) {
  if (!v) return
  const cur = t.get(name) ?? { earned: 0, registered: 0, planned: 0 }
  cur[fill] += v
  t.set(name, cur)
}

/** 色の付く区分はその名前、付かない区分・分からない科目は「その他」にまとめた集計を、色の順の Segment にする。 */
function toSegments(t: Totals, colors: CategoryColors): Segment[] {
  const merged: Totals = new Map()
  for (const [name, v] of t) {
    const key = colors.named(name) ? name : OTHER
    for (const f of ['earned', 'registered', 'planned'] as Fill[]) add(merged, key, f, v[f])
  }
  const out: Segment[] = []
  for (const name of [...colors.order, ...(colors.order.includes(OTHER) ? [] : [OTHER])]) {
    const v = merged.get(name)
    if (!v) continue
    for (const f of ['earned', 'registered', 'planned'] as Fill[]) {
      if (v[f] > 0) out.push({ key: name, color: colors.color(name === OTHER ? undefined : name), fill: f, value: v[f], label: name })
    }
  }
  return out
}

/** 卒業までの全体（修得・今期の登録・計画）を区分ごとに。 */
export function totalSegments(st: GraduationStatus, colors: CategoryColors = categoryColors(st)): Segment[] {
  const t: Totals = new Map()
  let earned = 0
  for (const r of st.requirements) {
    if (r.kind === 'item' && r.earned) {
      add(t, r.name, 'earned', r.earned)
      earned += r.earned
    }
  }
  // 個別成績表の区分の合計と【合計】がずれる分（卒業要件に数える区分の外など）は、その他に入れない（合計の数字を優先）
  if (st.total_earned > earned) add(t, OTHER, 'earned', st.total_earned - earned)
  for (const y of st.plan?.years ?? []) {
    for (const term of y.terms) {
      if (term.state === 'current') {
        for (const c of term.taken) add(t, requirementOf(st, c.category) ?? OTHER, 'registered', c.credits)
      }
      if (term.state === 'current' || term.state === 'future') {
        for (const it of term.items) add(t, it.category, 'planned', it.credits)
      }
    }
  }
  return toSegments(t, colors)
}

/** 1 学期分（終わった学期は修得、今期は登録と計画、来期以降は計画）を区分ごとに。 */
export function termSegments(st: GraduationStatus, term: PlanYearTerm, colors: CategoryColors): Segment[] {
  const t: Totals = new Map()
  for (const c of term.taken) {
    if (term.state === 'current' && !c.grade) add(t, requirementOf(st, c.category) ?? OTHER, 'registered', c.credits)
    else if (c.passed) add(t, requirementOf(st, c.category) ?? OTHER, 'earned', c.credits)
  }
  if (term.state !== 'past') for (const it of term.items) add(t, it.category, 'planned', it.credits)
  return toSegments(t, colors)
}
