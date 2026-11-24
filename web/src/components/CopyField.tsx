import { Check, Copy } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

async function copyText(text: string) {
  if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text)
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  document.execCommand('copy')
  ta.remove()
}

/** 値とコピーのボタン。block なら複数行のコマンド。 */
export function CopyField({ value, block }: { value: string; block?: boolean }) {
  const [done, setDone] = useState(false)
  const copy = () => copyText(value).then(() => {
    setDone(true)
    setTimeout(() => setDone(false), 1200)
  })
  return (
    <div className={cn('flex items-start gap-1 rounded-md border bg-muted/40', block ? 'p-2' : 'px-2 py-1')}>
      <code className={cn('min-w-0 flex-1 font-mono text-xs break-all', block && 'whitespace-pre-wrap')}>{value}</code>
      <Button type="button" size="icon-sm" variant="ghost" onClick={copy} aria-label={done ? 'コピーしました' : 'コピー'} className="shrink-0">
        {done ? <Check className="text-good" /> : <Copy />}
      </Button>
    </div>
  )
}
