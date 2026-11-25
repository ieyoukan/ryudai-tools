import { useQuery } from '@tanstack/react-query'
import { CheckCircle2, ChevronDown, Clock, Download, ExternalLink, FileArchive, FileJson, FileText, XCircle } from 'lucide-react'
import { useState } from 'react'
import { useParams } from 'wouter'

import type { AttendanceRow, ContentsData } from '@/api/gen/ui'
import { usePage, useRefreshFromWebClass } from '@/api/page'
import { RefreshButton } from '@/components/RefreshButton'
import { BackLink } from '@/components/BackLink'
import { EmptyState, ErrorState, LoadingBlock, Notice, PageHeader, SectionTitle, formatDate } from '@/components/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

type ContentFiles = { contentId: string; files: { name: string; downloadUrl: string }[] }[]

/** コースの資料と出席確認。資料のファイルは一覧を出してから順に読み込む。 */
export function ContentsPage() {
  const { courseID } = useParams<{ courseID: string }>()
  const name = new URLSearchParams(location.search).get('name') ?? ''
  const { data } = usePage<ContentsData>(contentsPath(courseID, name))
  return (
    <div className="space-y-4">
      <PageHeader back={<BackLink href="/courses">時間割</BackLink>} title={data?.CourseName || name || courseID} />
      <CourseContents courseID={courseID} name={name} />
    </div>
  )
}

const contentsPath = (courseID: string, name: string) => `/courses/${encodeURIComponent(courseID)}?name=${encodeURIComponent(name)}`

/** WebClass のコースの出席確認と資料。講義ページの WebClass タブでも使う。 */
export function CourseContents({ courseID, name }: { courseID: string; name: string }) {
  const { data, error, isPending, refetch } = usePage<ContentsData>(contentsPath(courseID, name))
  const files = useQuery({
    queryKey: ['course-files', courseID],
    queryFn: async () => {
      const r = await fetch(`/courses/${encodeURIComponent(courseID)}/files.json`, { credentials: 'same-origin' })
      if (!r.ok) throw new Error(`HTTP ${r.status}`)
      return ((await r.json()) ?? []) as ContentFiles
    },
    enabled: !!data && !data.Error && (data.Contents ?? []).length > 0,
    staleTime: 5 * 60_000,
  })
  const { refresh, refreshing } = useRefreshFromWebClass([contentsPath(courseID, name)], [{ url: `/courses/${encodeURIComponent(courseID)}/files.json`, queryKey: ['course-files', courseID] }])
  const byId = new Map((files.data ?? []).map((c) => [c.contentId, c.files]))
  const title = data?.CourseName || name || courseID
  const q = `?name=${encodeURIComponent(title)}`

  return (
    <div className="space-y-4">
      {isPending && <LoadingBlock label="WebClass から読み込んでいます" />}
      {error && <ErrorState message={error.message} onRetry={() => refetch()} />}
      {data?.Error && <ErrorState message={data.Error} onRetry={() => refetch()} />}

      {data && !data.Error && (
        <>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-xs text-muted-foreground">WebClass: {title}</p>
            <div className="flex flex-wrap gap-2">
              <RefreshButton onClick={refresh} refreshing={refreshing} />
              <Button asChild variant="outline" size="sm"><a href={data.WebClassURL} target="_blank" rel="noopener"><ExternalLink />WebClass で開く</a></Button>
            </div>
          </div>
          {data.AttendanceError ? (
            <Notice tone="warning">出席確認を取得できませんでした: {data.AttendanceError}</Notice>
          ) : (data.Attendance ?? []).length > 0 && (
            <Card>
              <CardContent className="space-y-3">
                <SectionTitle>出席確認</SectionTitle>
                <ul className="divide-y">
                  {data.Attendance.map((a, i) => <AttendanceItem key={i} a={a} />)}
                </ul>
              </CardContent>
            </Card>
          )}

          <Card>
            <CardContent className="space-y-3">
              <SectionTitle
                actions={(data.Contents ?? []).length > 0 && (
                  <div className="flex flex-wrap gap-2">
                    <Button asChild size="sm"><a href={`/courses/${encodeURIComponent(courseID)}/files.zip${q}`}><FileArchive />まとめてダウンロード</a></Button>
                    <Button asChild size="sm" variant="ghost" className="text-muted-foreground"><a href={`/courses/${encodeURIComponent(courseID)}/summary${q}`} target="_blank" rel="noopener"><FileJson />AI 要約用</a></Button>
                  </div>
                )}
              >
                資料
              </SectionTitle>
              {(data.Contents ?? []).length === 0 ? (
                <EmptyState>
                  資料が見つかりませんでした。
                  <span className="text-xs">試験・レポート・アンケートなどは表示していません。WebClass で開いてください。</span>
                </EmptyState>
              ) : (
                <ul className="divide-y rounded-lg border">
                  {data.Contents.map((c) => (
                    <ContentRow key={c.id} name={c.name} category={c.category} files={byId.get(c.id)} loading={files.isPending} failed={files.isError} />
                  ))}
                </ul>
              )}
              <p className="text-xs text-muted-foreground">資料だけを表示しています。試験・レポート・アンケートなどは WebClass で直接開いてください。</p>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}

function AttendanceItem({ a }: { a: AttendanceRow }) {
  const done = a.Status === '出席'
  const window = formatDate(a.WindowOpen) ? `${formatDate(a.WindowOpen)} 〜 ${formatDate(a.WindowClose).split(' ')[1] ?? ''}` : ''
  return (
    <li className="flex min-h-12 items-center gap-3 py-2">
      {done ? <CheckCircle2 className="size-4 shrink-0 text-good" /> : a.IsOpen ? <Clock className="size-4 shrink-0 text-warning" /> : <XCircle className="size-4 shrink-0 text-muted-foreground" />}
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm">{a.Name}</p>
        {window && <p className="text-xs tabular-nums text-muted-foreground">{window}</p>}
      </div>
      <Badge variant={a.IsOpen && !done ? 'default' : 'outline'}>{done ? '出席済み' : a.IsOpen ? '受付中' : a.Status || '締切'}</Badge>
    </li>
  )
}

function ContentRow({ name, category, files, loading, failed }: { name: string; category?: string; files?: { name: string; downloadUrl: string }[]; loading: boolean; failed: boolean }) {
  const [open, setOpen] = useState(false)
  const count = files?.length ?? 0
  return (
    <li>
      <button
        type="button"
        className={cn('flex min-h-12 w-full items-center gap-3 px-3 py-2 text-left', count > 0 && 'hover:bg-muted/50')}
        onClick={() => count > 0 && setOpen((v) => !v)}
        aria-expanded={count > 0 ? open : undefined}
        disabled={count === 0}
      >
        <FileText className="size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm">{name}</p>
          {category && <p className="text-xs text-muted-foreground">{category}</p>}
        </div>
        {loading ? <Skeleton className="h-4 w-12" /> : failed ? (
          <span className="text-xs text-muted-foreground">取得失敗</span>
        ) : count === 0 ? (
          <span className="text-xs text-muted-foreground">ファイルなし</span>
        ) : (
          <span className="flex items-center gap-1 text-xs text-muted-foreground">{count} 件<ChevronDown className={cn('size-4 transition-transform', open && 'rotate-180')} /></span>
        )}
      </button>
      {open && files && (
        <ul className="space-y-1 border-t bg-muted/30 px-3 py-2">
          {files.map((f) => (
            <li key={f.downloadUrl} className="flex items-center gap-2">
              <span className="min-w-0 flex-1 truncate text-sm" title={f.name}>{f.name}</span>
              <Button asChild size="sm" variant="outline" className="h-9 md:h-7"><a href={f.downloadUrl} download><Download />保存</a></Button>
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}
