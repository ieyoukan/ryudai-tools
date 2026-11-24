import type { GraduationStatus } from '@/api/gen/academic'

// グラフで区分ごとに使う色。量（修得 + 今期の登録 + 計画）の多い区分から 7 色を順に割り当て、残りは「その他」。
// 色は区分に結びつけ、ページをまたいでも同じ区分は同じ色にする。

export const OTHER = 'その他'
const SLOTS = 7

export type CategoryColors = {
  /** 区分名 → CSS の色（var(--cat-N)） */
  color: (name: string | undefined) => string
  /** 色を割り当てた順の区分名（凡例用。最後にその他） */
  order: string[]
  /** 色の付く区分か（その他にまとめるときの判定） */
  named: (name: string | undefined) => boolean
}

export function categoryColors(st: GraduationStatus): CategoryColors {
  const items = new Set(st.requirements.filter((r) => r.kind === 'item').map((r) => r.name))
  const amount = new Map<string, number>()
  for (const c of st.plan?.categories ?? []) {
    if (items.has(c.name)) amount.set(c.name, c.earned + c.registered + c.planned)
  }
  for (const r of st.requirements) {
    if (r.kind === 'item' && !amount.has(r.name)) amount.set(r.name, r.earned ?? 0)
  }
  const ranked = [...amount.entries()].filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1]).map(([k]) => k)
  const top = ranked.slice(0, SLOTS)
  const index = new Map(top.map((n, i) => [n, i + 1]))
  return {
    color: (name) => (name && index.has(name) ? `var(--cat-${index.get(name)})` : 'var(--cat-other)'),
    order: ranked.length > SLOTS ? [...top, OTHER] : top,
    named: (name) => !!name && index.has(name),
  }
}

/** 成績・履修登録の科目区分（例: 専門教育科目（知能情報コア））を、個別成績表の区分名にする。分からなければ undefined。 */
export function requirementOf(st: GraduationStatus, rawCategory: string | undefined): string | undefined {
  if (!rawCategory) return undefined
  return st.category_items?.[rawCategory]
}
