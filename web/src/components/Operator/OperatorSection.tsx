import { useOperator } from '../../data/useOperator'
import { OperatorView } from './OperatorView'

/** The operator console loads only while its view is open and refreshes every minute (Change 30). */
export default function OperatorSection({ now }: { now: number }) {
  const op = useOperator()
  return (
    <OperatorView
      status={op.status}
      error={op.error}
      documents={op.documents}
      commands={op.commands}
      incidents={op.incidents}
      audit={op.audit}
      now={now}
      onReload={op.reload}
    />
  )
}
