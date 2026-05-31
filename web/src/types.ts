export type BranchPhase = 'Pending' | 'Creating' | 'Ready' | 'Error' | 'Deleting' | ''

export interface Branch {
  name: string
  status: BranchPhase
  message?: string
  host?: string
  port?: number
  dsn?: string
  cluster_host?: string
  cluster_port?: number
  snapshot_ref?: string
  ttl_hours?: number
  database_type?: string
  created_at: string
  expires_at?: string
}

export interface Stats {
  total: number
  ready: number
  creating: number
  error: number
  pending: number
  deleting: number
}

export interface PodInfo {
  phase: string
  ready: boolean
  message?: string
}

export interface BranchMetrics {
  threads_connected: number
  available: boolean
  error?: string
}

export interface PodResource {
  name: string
  branch: string
  phase: string
  ready: boolean
  restarts: number
  node?: string
  message?: string
  created_at: string
}

export interface PVCResource {
  name: string
  branch: string
  status: string
  capacity?: string
  created_at: string
}

export interface ServiceResource {
  name: string
  branch: string
  cluster_ip?: string
  node_port?: number
  created_at: string
}

export interface ClusterResources {
  pods: PodResource[]
  pvcs: PVCResource[]
  services: ServiceResource[]
}

export interface Snapshot {
  name: string
  created_at: string
  database_type?: string
  role?: 'current' | 'archived' | 'auto' | string
}
