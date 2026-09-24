import { useState } from 'react'
import { AddHostDialog } from '@/features/hosts/components/AddHostDialog'
import { SummaryView } from '@/features/summary/components/SummaryView'

export function SummaryPage() {
  const [addOpen, setAddOpen] = useState(false)
  return (
    <>
      <SummaryView onAddHost={() => setAddOpen(true)} />
      <AddHostDialog open={addOpen} onClose={() => setAddOpen(false)} />
    </>
  )
}
