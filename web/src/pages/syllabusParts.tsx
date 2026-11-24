import type { LectureDetails } from '@/api/gen/academic'
import type { Book } from '@/api/gen/kyomu'
import { Card, CardContent } from '@/components/ui/card'

export function SyllabusSection({ title, children }: { title: string; children?: string }) {
  if (!children) return null
  return (
    <section>
      <h2 className="mb-1.5 text-sm font-semibold">{title}</h2>
      <p className="whitespace-pre-wrap break-words text-sm leading-relaxed text-muted-foreground">{children}</p>
    </section>
  )
}

export function SyllabusBooks({ title, books, note, emptyText }: { title: string; books?: Book[]; note?: string; emptyText?: string }) {
  if (!books?.length && !note && !emptyText) return null
  return (
    <section>
      <h2 className="mb-1.5 text-sm font-semibold">{title}</h2>
      {!!books?.length && <ul className="space-y-3 break-words text-sm text-muted-foreground">
        {books.map((b, i) => (
          <li key={i}>
            <p className="font-medium text-foreground">{b.title}</p>
            <p>{[b.author, b.publisher, b.year].filter(Boolean).join(' / ')}</p>
            {b.isbn && <p className="text-xs">ISBN {b.isbn}</p>}
            {b.note && <p className="whitespace-pre-wrap">{b.note}</p>}
          </li>
        ))}
      </ul>}
      {note && <p className="mt-2 whitespace-pre-wrap break-words text-sm leading-relaxed text-muted-foreground">{note}</p>}
      {!books?.length && !note && <p className="text-sm text-muted-foreground">{emptyText}</p>}
    </section>
  )
}

/** 時間割から開く講義画面で、授業に必要なシラバス情報をまとめる。 */
export function LectureSyllabus({ syllabus: s }: { syllabus: LectureDetails }) {
  return (
    <Card>
      <CardContent className="divide-y [&>section]:py-4 [&>section:first-child]:pt-0 [&>section:last-child]:pb-0">
        <section>
          <h2 className="mb-1.5 text-sm font-semibold">授業計画</h2>
          {s.plan ? (
            <details>
              <summary className="flex min-h-11 cursor-pointer items-center text-sm text-primary hover:underline">授業計画の原文を表示</summary>
              <p className="mt-2 whitespace-pre-wrap break-words text-sm leading-relaxed text-muted-foreground">{s.plan}</p>
            </details>
          ) : <p className="text-sm text-muted-foreground">シラバスに授業計画の記載がありません。</p>}
        </section>
        <SyllabusSection title="評価基準と評価方法">{s.grading || 'シラバスに評価基準と評価方法の記載がありません。'}</SyllabusSection>
        <SyllabusBooks title="教科書" books={s.textbooks} note={s.textbook_note} emptyText="シラバスに教科書の記載がありません。" />
        <SyllabusBooks title="参考書" books={s.references} note={s.reference_note} />
        <SyllabusSection title="事前学習">{s.preparation}</SyllabusSection>
        <SyllabusSection title="事後学習">{s.review}</SyllabusSection>
        <section className="text-xs text-muted-foreground">
          教務システムのシラバスから取得。授業計画は変更される場合があります。
        </section>
      </CardContent>
    </Card>
  )
}
