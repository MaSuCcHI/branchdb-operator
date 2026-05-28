import type { K8sBranch, K8sStats, PodInfo, BranchMetrics, K8sSnapshot } from './k8sTypes'

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

export const k8sApi = {
  branches: {
    list: () => request<K8sBranch[]>('/branches'),
    get: (name: string) => request<K8sBranch>(`/branches/${name}`),
    create: (body: { name: string; snapshot_ref?: string; ttl_hours?: number }) =>
      request<K8sBranch>('/branches', { method: 'POST', body: JSON.stringify(body) }),
    delete: (name: string) => request<void>(`/branches/${name}`, { method: 'DELETE' }),
    getPod: (name: string) => request<PodInfo>(`/branches/${name}/pod`),
    getMetrics: (name: string) => request<BranchMetrics>(`/branches/${name}/metrics`),
  },
  stats: {
    get: () => request<K8sStats>('/stats'),
  },
  snapshots: {
    list: () => request<K8sSnapshot[]>('/snapshots'),
    take: () => request<K8sSnapshot>('/snapshots', { method: 'POST' }),
  },
}
