import { createContext, useContext, useEffect, useMemo, useSyncExternalStore, type ReactNode } from 'react'
import type { DashboardState } from './model'
import { StreamClient } from './stream'
import { DashboardStore } from './store'

const StoreContext = createContext<DashboardStore | null>(null)

/** Owns the dashboard store and its live stream for as long as the dashboard is mounted. */
export function DashboardProvider({ children, store: injected }: { children: ReactNode; store?: DashboardStore }) {
  const store = useMemo(() => injected ?? new DashboardStore(), [injected])
  useEffect(() => {
    if (injected) return // tests drive an injected store themselves
    store.start()
    const stream = new StreamClient(store)
    stream.start()
    return () => {
      stream.stop()
      store.stop()
    }
  }, [store, injected])
  return <StoreContext.Provider value={store}>{children}</StoreContext.Provider>
}

export function useStore(): DashboardStore {
  const store = useContext(StoreContext)
  if (!store) throw new Error('useStore must be used inside DashboardProvider')
  return store
}

export function useDashboard(): DashboardState {
  const store = useStore()
  return useSyncExternalStore(store.subscribe, store.getSnapshot)
}
