import { useSales } from '../../data/useSales'
import { SalesView } from './SalesView'

/** Sales loads only while its view is open: a summary changes hourly, nothing streams it (Change 27). */
export default function SalesSection({ now }: { now: number }) {
  const sales = useSales()
  return <SalesView status={sales.status} error={sales.error} documents={sales.documents} now={now} onRetry={sales.reload} />
}
