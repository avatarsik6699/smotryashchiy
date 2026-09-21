import { useEffect, useState } from 'react'

/** Current time in ms, refreshed every `intervalMs` so ages and freshness states keep ticking. */
export function useNow(intervalMs = 5000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(id)
  }, [intervalMs])
  return now
}
