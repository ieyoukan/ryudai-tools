import { Download, ExternalLink } from 'lucide-react'
import { useParams } from 'wouter'

import type { FilesData } from '@/api/gen/ui'
import { usePage } from '@/api/page'
import { BackLink } from '@/components/BackLink'
import { EmptyState, ErrorState, LoadingBlock, PageHeader } from '@/components/page'
import { Button } from '@/components/ui/button'

/** 1 つの資料のファイル（以前のリンクから来たとき用。普段はコースのページで開く）。 */
export function FilesPage() {
  const { courseID, contentID } = useParams<{ courseID: string; contentID: string }>()
  const sp = new URLSearchParams(location.search)
  const { data, error, isPending, refetch } = usePage<FilesData>(`/courses/${encodeURIComponent(courseID)}/contents/${encodeURIComponent(contentID)}${location.search}`)
  const course = data?.CourseName || sp.get('course') || ''
  return (
    <div className="space-y-4">
      <PageHeader back={<BackLink href={`/courses/${encodeURIComponent(courseID)}?name=${encodeURIComponent(course)}`}>{course || 'コース'}</BackLink>} title={data?.ContentName || sp.get('name') || contentID} />
      {isPending && <LoadingBlock rows={2} />}
      {error && <ErrorState message={error.message} onRetry={() => refetch()} />}
      {data?.Error && <ErrorState message={data.Error} />}
      {data && !data.Error && ((data.Files ?? []).length === 0 ? (
        <EmptyState>ダウンロードできるファイルはありませんでした。本文だけの資料は WebClass で開いてください。</EmptyState>
      ) : (
        <ul className="divide-y rounded-xl border bg-card">
          {data.Files.map((f) => (
            <li key={f.url} className="flex min-h-12 items-center gap-2 px-3 py-2">
              <span className="min-w-0 flex-1 truncate text-sm">{f.name}</span>
              <Button asChild size="sm" variant="outline"><a href={`/download?url=${encodeURIComponent(f.url)}&name=${encodeURIComponent(f.name)}`} download><Download />保存</a></Button>
            </li>
          ))}
        </ul>
      ))}
      {data?.WebClassURL && <Button asChild variant="ghost" size="sm"><a href={data.WebClassURL} target="_blank" rel="noopener"><ExternalLink />WebClass で開く</a></Button>}
    </div>
  )
}
