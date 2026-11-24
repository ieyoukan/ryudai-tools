import { useState, type ReactNode } from 'react'

import type { GraduationStatus } from '@/api/gen/academic'
import { useAddPlanItem } from '@/api/hooks'
import { Button } from '@/components/ui/button'
import {
  Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { credits } from '@/lib/format'

/** おすすめに無い科目を、科目名・区分・単位を入れて計画に足す。 */
export function AddItemDialog({ term, status, children }: { term: string; status: GraduationStatus; children: ReactNode }) {
  const categories = status.plan?.categories ?? []
  const firstShort = categories.find((c) => c.shortfall_after_plan > 0) ?? categories[0]
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [category, setCategory] = useState(firstShort ? firstShort.section + '|' + firstShort.name : '')
  const [units, setUnits] = useState('2')
  const add = useAddPlanItem()

  const n = Number(units)
  const valid = title.trim() !== '' && category !== '' && n > 0 && n <= 30
  const submit = () => {
    const [section, name] = category.split('|')
    add.mutate({ year_term: term, title: title.trim(), section, category: name, credits: n }, {
      onSuccess: () => {
        setOpen(false)
        setTitle('')
      },
    })
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{children}</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{term} に科目を追加</DialogTitle>
          <DialogDescription>科目名はシラバスどおりに入れてください（成績・履修登録と照らし合わせます）。</DialogDescription>
        </DialogHeader>
        <form className="grid gap-4" onSubmit={(e) => { e.preventDefault(); if (valid) submit() }}>
          <div className="grid gap-1.5">
            <Label htmlFor="plan-title">科目名</Label>
            <Input id="plan-title" className="h-10 md:h-9" value={title} maxLength={100} placeholder="例: 機械学習" autoFocus onChange={(e) => setTitle(e.target.value)} />
          </div>
          <div className="grid grid-cols-[1fr_6rem] gap-3">
            <div className="grid gap-1.5">
              <Label>区分</Label>
              <Select value={category} onValueChange={setCategory}>
                <SelectTrigger className="h-10 w-full md:h-9"><SelectValue placeholder="区分を選ぶ" /></SelectTrigger>
                <SelectContent>
                  {categories.map((c) => (
                    <SelectItem key={c.section + c.name} value={c.section + '|' + c.name}>
                      {c.name}（{c.section}）{c.shortfall_after_plan > 0 && ` ・ あと ${credits(c.shortfall_after_plan)}`}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="plan-credits">単位</Label>
              <Input id="plan-credits" className="h-10 md:h-9" inputMode="decimal" type="number" min={0.5} max={30} step={0.5} value={units} onChange={(e) => setUnits(e.target.value)} />
            </div>
          </div>
          <DialogFooter>
            <DialogClose asChild><Button type="button" variant="outline">やめる</Button></DialogClose>
            <Button type="submit" disabled={!valid || add.isPending}>追加する</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
