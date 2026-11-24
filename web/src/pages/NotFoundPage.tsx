import { Link } from 'wouter'

import { Button } from '@/components/ui/button'

export function NotFoundPage() {
  return (
    <div className="flex flex-col items-center gap-3 py-16 text-center">
      <p className="text-4xl font-semibold text-muted-foreground">404</p>
      <p className="text-sm text-muted-foreground">このページは見つかりませんでした。</p>
      <Button asChild variant="outline"><Link href="/courses">時間割へ</Link></Button>
    </div>
  )
}
