import { RefreshCw } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

/** WebClass から最新の内容を取り直すボタン。表示は数分〜数時間キャッシュしているため。 */
export function RefreshButton({ onClick, refreshing }: { onClick: () => void; refreshing: boolean }) {
  return (
    <Button variant="outline" size="sm" onClick={onClick} disabled={refreshing} title="WebClass から最新の内容を取り直します">
      <RefreshCw className={cn(refreshing && 'animate-spin')} />
      {refreshing ? '取得中…' : '最新にする'}
    </Button>
  )
}
