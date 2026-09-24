import { useParams } from 'react-router'
import { AlertDetailView } from '@/features/alerts/components/AlertDetailView'

export function AlertDetailPage() {
  const { id = '' } = useParams()
  return <AlertDetailView id={id} />
}
