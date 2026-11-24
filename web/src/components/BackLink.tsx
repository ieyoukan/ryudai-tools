import { ChevronLeft } from 'lucide-react'
import { Link } from 'wouter'

export function BackLink({ href, children }: { href: string; children: string }) {
  return (
    <Link href={href} className="-ml-1 inline-flex items-center gap-0.5 rounded-md px-1 py-1 text-sm text-muted-foreground hover:text-foreground">
      <ChevronLeft className="size-4" />{children}
    </Link>
  )
}
