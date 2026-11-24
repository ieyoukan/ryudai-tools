import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { api } from './client'
import type { GraduationStatus, PlanSuggestions } from './gen/academic'
import type { PlanItem } from './gen/db'

const graduationKey = ['graduation'] as const

/** 成績・個別成績表・今期の登録・履修計画をまとめた卒業要件の見通し（初回は教務システムから 10〜20 秒）。 */
export function useGraduation() {
  return useQuery({
    queryKey: graduationKey,
    queryFn: () => api.get<GraduationStatus>('/api/me/graduation'),
    staleTime: 60_000,
    retry: false,
  })
}

/** 成績一覧つきの見通し（成績ページ用）。キャッシュは見通しと別に持つが、計画を変えると一緒に読み直す。 */
export function useGraduationWithGrades() {
  return useQuery({
    queryKey: [...graduationKey, 'grades'],
    queryFn: () => api.get<GraduationStatus>('/api/me/graduation?include_grades=1'),
    staleTime: 60_000,
    retry: false,
  })
}

/** term（例: 2027前期）の計画に足す候補の科目。enabled になってから読み込む。 */
export function useSuggestions(term: string, enabled: boolean) {
  return useQuery({
    queryKey: ['suggestions', term],
    queryFn: () => api.get<PlanSuggestions>('/api/me/plan/suggestions?term=' + encodeURIComponent(term)),
    enabled,
    staleTime: 60_000,
    retry: false,
  })
}

/** 計画を書き換えたら、見通しと候補を読み直す。 */
function useRefresh() {
  const qc = useQueryClient()
  return () => Promise.all([
    qc.invalidateQueries({ queryKey: graduationKey }),
    qc.invalidateQueries({ queryKey: ['suggestions'] }),
  ])
}

export type NewPlanItem = Pick<PlanItem, 'year_term' | 'title' | 'section' | 'category' | 'credits'>

export function useAddPlanItem() {
  const refresh = useRefresh()
  return useMutation({
    mutationFn: (it: NewPlanItem) => api.post<void>('/api/me/plan/items', it),
    onSuccess: (_, it) => {
      toast.success(`「${it.title}」を ${it.year_term} の計画に追加しました`)
      return refresh()
    },
    onError: (e) => toast.error(e.message),
  })
}

export function useDeletePlanItem() {
  const refresh = useRefresh()
  return useMutation({
    mutationFn: (it: PlanItem) => api.del<void>('/api/me/plan/items/' + it.id),
    onSuccess: (_, it) => {
      toast.success(`「${it.title}」を計画から外しました`)
      return refresh()
    },
    onError: (e) => toast.error(e.message),
  })
}

export function useDeletePlan() {
  const refresh = useRefresh()
  return useMutation({
    mutationFn: () => api.del<void>('/api/me/plan/items'),
    onSuccess: () => {
      toast.success('計画をすべて削除しました')
      return refresh()
    },
    onError: (e) => toast.error(e.message),
  })
}

/** 今期の登録科目の区分（講義コード → 個別成績表の区分名）を保存する。 */
export function useSaveLectureCategories() {
  const refresh = useRefresh()
  return useMutation({
    mutationFn: (categories: Record<string, string>) => api.put<void>('/api/me/lecture-categories', { categories }),
    onSuccess: () => {
      toast.success('区分を保存しました')
      return refresh()
    },
    onError: (e) => toast.error(e.message),
  })
}
