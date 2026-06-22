import { useEffect, useRef, useState } from 'react'
import type {
  ComplianceResp,
  Congestion,
  DeviceTrendResponse,
  Flow,
  Interface,
  LivePayload,
  ReclaimPort,
  Sample,
  Topology,
  TrendResponse,
  UpgradeStatus
} from './types'

export async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`)
  return (await res.json()) as T
}

export async function postJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, { method: 'POST' })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((data && (data as any).error) || `${url}: HTTP ${res.status}`)
  return data as T
}

export const api = {
  interfaces: (serial: string) =>
    getJSON<Interface[]>(`/api/devices/${encodeURIComponent(serial)}/interfaces`),
  deviceHistory: (serial: string) =>
    getJSON<Sample[]>(`/api/devices/${encodeURIComponent(serial)}/history`),
  interfaceHistory: (serial: string, name: string) =>
    getJSON<Sample[]>(
      `/api/devices/${encodeURIComponent(serial)}/interface-history?name=${encodeURIComponent(name)}`
    ),
  version: () => getJSON<UpgradeStatus>('/api/version'),
  checkUpgrade: () => postJSON<UpgradeStatus>('/api/upgrade/check'),
  applyUpgrade: () => postJSON<{ status: string; detail: string }>('/api/upgrade'),
  trend: (days = 30) => getJSON<TrendResponse>(`/api/trend?days=${days}`),
  deviceTrend: (serial: string, days = 30) =>
    getJSON<DeviceTrendResponse>(`/api/devices/${encodeURIComponent(serial)}/trend?days=${days}`),
  topology: (dc: string) => getJSON<Topology>(`/api/topology?dc=${encodeURIComponent(dc)}`),
  flows: (dc = '', limit = 30) => getJSON<Flow[]>(`/api/flows?dc=${encodeURIComponent(dc)}&limit=${limit}`),
  congestion: (dc = '', limit = 50) =>
    getJSON<Congestion[]>(`/api/congestion?dc=${encodeURIComponent(dc)}&limit=${limit}`),
  reclaim: (days = 30) => getJSON<ReclaimPort[]>(`/api/reclaim?days=${days}`),
  compliance: () => getJSON<ComplianceResp>('/api/compliance')
}

// useLive subscribes to the SSE stream and returns the latest fleet snapshot
// plus a connection flag. It transparently reconnects on disconnect.
export function useLive(): { live: LivePayload | null; connected: boolean } {
  const [live, setLive] = useState<LivePayload | null>(null)
  const [connected, setConnected] = useState(false)
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    let stopped = false
    let retry: ReturnType<typeof setTimeout>

    const connect = () => {
      if (stopped) return
      const es = new EventSource('/api/stream')
      esRef.current = es
      es.addEventListener('snapshot', (ev) => {
        try {
          setLive(JSON.parse((ev as MessageEvent).data))
          setConnected(true)
        } catch {
          /* ignore malformed frame */
        }
      })
      es.onopen = () => setConnected(true)
      es.onerror = () => {
        setConnected(false)
        es.close()
        retry = setTimeout(connect, 3000)
      }
    }
    connect()

    return () => {
      stopped = true
      clearTimeout(retry)
      esRef.current?.close()
    }
  }, [])

  return { live, connected }
}

// usePolling re-runs an async loader on an interval and returns its result.
export function usePolling<T>(loader: () => Promise<T>, deps: unknown[], intervalMs = 5000) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let active = true
    const run = () => {
      loader()
        .then((d) => {
          if (active) {
            setData(d)
            setError(null)
          }
        })
        .catch((e) => active && setError(String(e)))
    }
    run()
    const id = setInterval(run, intervalMs)
    return () => {
      active = false
      clearInterval(id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { data, error }
}
