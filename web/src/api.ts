import type { Branch, Stats, PodInfo, BranchMetrics, Snapshot } from './types'

async function request<T>(path: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  })
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText)
    throw new Error(`${res.status}: ${text}`)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export const api = {
  branches: {
    list: () => request<Branch[]>('/branches'),
    get: (name: string) => request<Branch>(`/branches/${name}`),
    create: (body: { name: string; snapshot_ref?: string; ttl_hours?: number; database_type?: string }) =>
      request<Branch>('/branches', { method: 'POST', body: JSON.stringify(body) }),
    delete: (name: string) => request<void>(`/branches/${name}`, { method: 'DELETE' }),
    getPod: (name: string) => request<PodInfo>(`/branches/${name}/pod`),
    getMetrics: (name: string) => request<BranchMetrics>(`/branches/${name}/metrics`),
  },
  stats: {
    get: () => request<Stats>('/stats'),
  },
  snapshots: {
    list: (dbType?: string) => request<Snapshot[]>(`/snapshots${dbType ? `?db_type=${encodeURIComponent(dbType)}` : ''}`),
    take: (body: { db_type?: string; name?: string; overwrite?: boolean }) =>
      request<{ status: string; name: string }>('/snapshots', { method: 'POST', body: JSON.stringify(body) }),
  },
}
