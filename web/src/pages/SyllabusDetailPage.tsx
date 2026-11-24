import { ExternalLink } from 'lucide-react'
import type { ReactNode } from 'react'
import { useParams } from 'wouter'

import type { SyllabusDetailData } from '@/api/gen/ui'
import { usePage } from '@/api/page'
import { BackLink } from '@/components/BackLink'
import { ErrorState, LoadingBlock, PageHeader } from '@/components/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { credits } from '@/lib/format'
import { SeatInfo } from './lectureParts'
import { SyllabusBooks as Books, SyllabusSection as Sec } from './syllabusParts'

export function SyllabusDetailPage() {
  const { year, code } = useParams<{ year: string; code: string }>()
  const { data, error, isPending } = usePage<SyllabusDetailData>(`/syllabus/${year}/${encodeURIComponent(code)}`, { staleTime: 10 * 60_000 })
  const s = data?.Syllabus
  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <PageHeader
        back={<BackLink href="/syllabus">シラバス検索</BackLink>}
        title={s?.title ?? 'シラバス'}
        actions={s?.url && <Button asChild variant="outline" size="sm"><a href={s.url} target="_blank" rel="noopener"><ExternalLink />教務システムで開く</a></Button>}
      />
      {isPending && <LoadingBlock />}
      {error && <ErrorState message={error.message} />}
      {data?.Error && <ErrorState message={data.Error} />}
      {s && (
        <>
          <Card>
            <CardContent className="space-y-4">
              <div className="flex flex-wrap items-baseline gap-x-6 gap-y-2">
                <p className="text-3xl font-semibold">{credits(s.credits)}<span className="ml-1 text-sm font-normal text-muted-foreground">単位</span></p>
                <p className="text-sm">{s.year}年度 {s.term} {s.day_period}</p>
                <div className="flex flex-wrap gap-1.5">
                  {s.main_course && <Badge variant="outline">主要授業科目</Badge>}
                  {s.teaching_format && <Badge variant="outline">{s.teaching_format}</Badge>}
                  {(s.lecture_types ?? []).map((t) => <Badge key={t} variant="outline">{t}</Badge>)}
                </div>
              </div>
              {s.availability && <SeatInfo a={s.availability} />}
              <dl className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-2 text-sm">
                <Fact label="担当教員">{s.instructors}</Fact>
                <Fact label="開講学部等">{s.faculty}</Fact>
                {s.room && <Fact label="教室">{s.room}</Fact>}
                {s.registration_method && <Fact label="履修登録方法">{s.registration_method}</Fact>}
                {s.availability?.method && <Fact label="現在の登録方法">{s.availability.method}</Fact>}
                {s.availability?.note && s.availability.status !== 'registered_not_listed' && <Fact label="登録情報の補足">{s.availability.note}</Fact>}
                {s.availability?.status === 'registered_not_listed' && <Fact label="人数情報">登録済み科目の登録者数・残席は、現在の教務画面から取得できません。</Fact>}
                <Fact label="講義コード">{s.lecture_code}{s.numbering_code && `（科目番号 ${s.numbering_code}）`}</Fact>
                {s.language && <Fact label="使用言語">{s.language}</Fact>}
              </dl>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="divide-y [&>section]:py-4 [&>section:first-child]:pt-0 [&>section:last-child]:pb-0">
              <Sec title="授業内容と方法">{s.outline}</Sec>
              <Sec title="達成目標">{s.goals}</Sec>
              <Sec title="評価基準と評価方法">{s.grading}</Sec>
              <Sec title="履修条件">{s.requirements}</Sec>
              <Sec title="授業計画">{s.plan}</Sec>
              <Sec title="事前学習">{s.preparation}</Sec>
              <Sec title="事後学習">{s.review}</Sec>
              <Books title="教科書" books={s.textbooks} note={s.textbook_note} />
              <Books title="参考書" books={s.references} note={s.reference_note} />
              <Sec title="メッセージ">{s.message}</Sec>
              <Sec title="オフィスアワー">{s.office_hours}</Sec>
              <Sec title="URGCC 学習教育目標">{s.urgcc}</Sec>
            </CardContent>
          </Card>
          {s.website && <Button asChild variant="ghost" size="sm"><a href={s.website} target="_blank" rel="noopener"><ExternalLink />授業の URL</a></Button>}
        </>
      )}
    </div>
  )
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </>
  )
}
