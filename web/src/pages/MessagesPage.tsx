import { Paperclip } from 'lucide-react'

import type { MessagesData } from '@/api/gen/ui'
import { usePage } from '@/api/page'
import { EmptyState, ErrorState, LoadingBlock, PageHeader } from '@/components/page'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

/** WebClass の受信箱。押すと WebClass でメッセージを開く。 */
export function MessagesPage() {
  const { data, error, isPending, refetch } = usePage<MessagesData>('/messages')
  return (
    <div className="space-y-4">
      <PageHeader
        title={<span className="flex items-center gap-2">メッセージ{!!data?.UnreadCnt && <Badge>{data.UnreadCnt} 件の未読</Badge>}</span>}
        description="自分宛の自動通知（レポート受取など）は除いています。"
      />
      {isPending && <LoadingBlock />}
      {error && <ErrorState message={error.message} onRetry={() => refetch()} />}
      {data?.Error && <ErrorState message={data.Error} onRetry={() => refetch()} />}
      {data && !data.Error && ((data.Messages ?? []).length === 0 ? (
        <EmptyState>メッセージはありません。</EmptyState>
      ) : (
        <ul className="divide-y rounded-xl border bg-card">
          {data.Messages.map((m) => (
            <li key={m.id}>
              <a href={m.view_url} target="_blank" rel="noopener" className="flex min-h-14 items-start gap-3 px-4 py-3 hover:bg-muted/50">
                <span className={cn('mt-1.5 size-2 shrink-0 rounded-full', m.is_read ? 'bg-transparent' : 'bg-primary')} aria-label={m.is_read ? undefined : '未読'} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-baseline justify-between gap-2">
                    <p className={cn('truncate text-sm', !m.is_read && 'font-semibold')}>{m.from}</p>
                    <p className="shrink-0 text-xs tabular-nums text-muted-foreground">{m.date}</p>
                  </div>
                  <p className="flex items-center gap-1 truncate text-sm text-muted-foreground">
                    {m.has_attachment && <Paperclip className="size-3.5 shrink-0" aria-label="添付あり" />}
                    {m.subject}
                  </p>
                </div>
              </a>
            </li>
          ))}
        </ul>
      ))}
    </div>
  )
}
