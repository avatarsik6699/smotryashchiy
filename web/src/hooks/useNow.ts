import { useEffect, useState } from 'react'

/**
 * Current time in ms, refreshed every `intervalMs` so ages and freshness states keep ticking. A
 * hidden tab does not tick (nothing is on screen to age); it catches up the moment it is shown
 * (Change 38).
 */
export function useNow(intervalMs = 5000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const tick = () => {
      if (!document.hidden) setNow(Date.now())
    }
    const id = setInterval(tick, intervalMs)
    document.addEventListener('visibilitychange', tick)
    return () => {
      clearInterval(id)
      document.removeEventListener('visibilitychange', tick)
    }
  }, [intervalMs])
  return now
}
