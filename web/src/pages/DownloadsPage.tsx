import { Download } from 'lucide-react'

import { PageHeader } from '@/components/page'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { releasesURL } from '@/lib/project'

export function DownloadsPage() {
  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <PageHeader title="ダウンロード" description="Ryudai Tools を自分のパソコンやサーバーで動かせます。" />
      <Button asChild>
        <a href={releasesURL}><Download />GitHubで最新版をダウンロード</a>
      </Button>
      <Card>
        <CardContent className="space-y-2 text-sm">
          <h2 className="font-semibold">使い方</h2>
          <ol className="list-decimal space-y-1 pl-5 text-muted-foreground">
            <li>GitHub Releases の Assets から、ご利用のOSとCPUに合うアーカイブをダウンロードして展開します。SHA-256は <code>checksums.txt</code> で確認できます。</li>
            <li>展開したフォルダで <code>ryudai-tools</code>（Windowsでは <code>ryudai-tools.exe</code>）を起動します。</li>
            <li>ブラウザで <code>http://localhost:8080</code> を開くとログインできます。データは <code>./data/</code> に保存されます。</li>
          </ol>
          <p className="text-muted-foreground">ポートなどの設定は、同じフォルダの <code>.env</code> で変更できます。</p>
        </CardContent>
      </Card>
    </div>
  )
}
