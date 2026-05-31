import { useState, useEffect, useCallback, useRef } from 'react'
import { api } from './api'
import type { Branch, Stats, Snapshot, PodInfo, BranchMetrics } from './types'
import './App.css'

// ── Utility ─────────────────────────────────────────────────────────────────

function relativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

function ttlLabel(branch: Branch): string {
  if (!branch.expires_at) return 'no expiry'
  const ms = new Date(branch.expires_at).getTime() - Date.now()
  if (ms <= 0) return 'expired'
  const hrs = Math.ceil(ms / 3600000)
  if (hrs < 24) return `expires in ${hrs}h`
  return `expires in ${Math.floor(hrs / 24)}d`
}

function phaseClass(phase: string): string {
  return phase.toLowerCase() || 'pending'
}

function hasInProgress(branches: Branch[]): boolean {
  return branches.some(b =>
    b.status === 'Creating' || b.status === 'Pending' || b.status === 'Deleting'
  )
}

// ── CopyButton ───────────────────────────────────────────────────────────────

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = () => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }
  return (
    <button
      className={`copy-btn${copied ? ' copied' : ''}`}
      onClick={e => { e.stopPropagation(); handleCopy() }}
    >
      {copied ? 'Copied!' : 'Copy'}
    </button>
  )
}

// ── PhaseBadge ───────────────────────────────────────────────────────────────

function PhaseBadge({ phase }: { phase: string }) {
  return <span className={`phase-badge ${phaseClass(phase)}`}>{phase || 'Creating'}</span>
}

// ── StatCard ─────────────────────────────────────────────────────────────────

function StatCard({ label, value, variant }: { label: string; value: number; variant: string }) {
  return (
    <div className={`stat-card ${variant}`}>
      <div className="stat-value">{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  )
}

// ── CreateModal ──────────────────────────────────────────────────────────────

interface CreateModalProps {
  onClose: () => void
  onCreate: (name: string, snapshotRef: string, ttlHours: number, dbType: string) => Promise<void>
}

function CreateModal({ onClose, onCreate }: CreateModalProps) {
  const [name, setName] = useState('')
  const [snapshotRef, setSnapshotRef] = useState('')
  const [ttlHours, setTtlHours] = useState(0)
  const [dbType, setDbType] = useState('mysql')
  const [snapshots, setSnapshots] = useState<Snapshot[]>([])
  const [snapshotsLoading, setSnapshotsLoading] = useState(false)
  const [snapshotsUnavailable, setSnapshotsUnavailable] = useState(false)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const nameRef = useRef<HTMLInputElement>(null)

  useEffect(() => { nameRef.current?.focus() }, [])

  useEffect(() => {
    setSnapshotsLoading(true)
    setSnapshotsUnavailable(false)
    setSnapshotRef('')
    setSnapshots([])
    api.snapshots.list(dbType)
      .then(snaps => {
        setSnapshots(snaps)
        if (snaps.length > 0) setSnapshotRef(snaps[0].name)
      })
      .catch(e => {
        if (String(e).includes('501')) setSnapshotsUnavailable(true)
        else setErr(String(e))
      })
      .finally(() => setSnapshotsLoading(false))
  }, [dbType])

  const canCreate = !snapshotsUnavailable && !snapshotsLoading && snapshotRef !== ''

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) { setErr('Name is required'); return }
    if (!canCreate) return
    setLoading(true)
    setErr('')
    try {
      await onCreate(name.trim(), snapshotRef, ttlHours, dbType)
      onClose()
    } catch (ex) {
      setErr(String(ex))
      setLoading(false)
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()}>
        <h2>Create Branch</h2>
        {err && <div className="error-banner">{err}</div>}
        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label>Name *</label>
            <input
              ref={nameRef}
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="feature-login"
              autoComplete="off"
            />
          </div>
          <div className="form-field">
            <label>Database Type</label>
            <select value={dbType} onChange={e => setDbType(e.target.value)}>
              <option value="mysql">MySQL</option>
              <option value="postgres">PostgreSQL</option>
              <option value="redis">Redis</option>
            </select>
          </div>
          <div className="form-field">
            <label>Snapshot *</label>
            {snapshotsUnavailable ? (
              <div className="form-hint form-hint-error">
                Snapshot API unavailable — set <code>ZFSDB_ZFSAGENT_URL</code> to enable
              </div>
            ) : snapshotsLoading ? (
              <div className="form-hint">Loading snapshots...</div>
            ) : snapshots.length === 0 ? (
              <div className="form-hint form-hint-error">No snapshots available for {dbType}</div>
            ) : (
              <select value={snapshotRef} onChange={e => setSnapshotRef(e.target.value)}>
                {snapshots.map(s => (
                  <option key={s.name} value={s.name}>
                    {s.name} — {new Date(s.created_at).toLocaleDateString()}
                  </option>
                ))}
              </select>
            )}
          </div>
          <div className="form-field">
            <label>TTL Hours</label>
            <input
              type="number"
              min={0}
              value={ttlHours}
              onChange={e => setTtlHours(parseInt(e.target.value) || 0)}
              placeholder="0"
            />
            <div className="form-hint">0 = no expiry</div>
          </div>
          <div className="modal-actions">
            <button type="button" className="btn" onClick={onClose} disabled={loading}>
              Cancel
            </button>
            <button type="submit" className="btn btn-primary" disabled={loading || !canCreate}>
              {loading ? 'Creating...' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ── DetailPanel ──────────────────────────────────────────────────────────────

function DetailPanel({ branch }: { branch: Branch }) {
  const [pod, setPod] = useState<PodInfo | null>(null)

  useEffect(() => {
    api.branches.getPod(branch.name).then(setPod).catch(() => null)
  }, [branch.name])

  return (
    <tr className="detail-row-tr">
      <td colSpan={8} className="detail-panel">
        <div className="detail-panel-inner">
          {/* Connection info */}
          <div className="detail-section">
            <h3>Connection</h3>
            <div className="detail-kv">
              {branch.cluster_host && (
                <div>
                  <div className="detail-key">Cluster DSN</div>
                  <div className="dsn-row">
                    <span className="dsn-text">root@tcp({branch.cluster_host}:3306)/</span>
                    <CopyButton text={`root@tcp(${branch.cluster_host}:3306)/`} />
                  </div>
                </div>
              )}
              {branch.dsn && (
                <div>
                  <div className="detail-key">External DSN</div>
                  <div className="dsn-row">
                    <span className="dsn-text">{branch.dsn}</span>
                    <CopyButton text={branch.dsn} />
                  </div>
                </div>
              )}
              {!branch.cluster_host && !branch.dsn && (
                <span className="loading-text">Not yet assigned</span>
              )}
            </div>
          </div>

          {/* Pod / Resources */}
          <div className="detail-section">
            <h3>Resources</h3>
            <div className="detail-kv">
              {pod ? (
                <>
                  <div className="detail-row">
                    <span className="detail-key">Pod Phase</span>
                    <span className="detail-val">
                      <PhaseBadge phase={pod.phase} />
                    </span>
                  </div>
                  {pod.message && (
                    <div className="detail-row">
                      <span className="detail-key">Message</span>
                      <span className="detail-val" style={{ color: 'var(--error)' }}>{pod.message}</span>
                    </div>
                  )}
                </>
              ) : (
                <span className="loading-text">Loading pod info...</span>
              )}
              {branch.message && (
                <div className="detail-row">
                  <span className="detail-key">Status Msg</span>
                  <span className="detail-val">{branch.message}</span>
                </div>
              )}
            </div>
          </div>

          {/* Spec */}
          <div className="detail-section">
            <h3>Spec</h3>
            <div className="detail-kv">
              {branch.snapshot_ref && (
                <div className="detail-row">
                  <span className="detail-key">SnapshotRef</span>
                  <span className="detail-val" style={{ fontFamily: 'monospace' }}>{branch.snapshot_ref}</span>
                </div>
              )}
              <div className="detail-row">
                <span className="detail-key">TTL Hours</span>
                <span className="detail-val">{branch.ttl_hours ? `${branch.ttl_hours}h` : 'No expiry'}</span>
              </div>
              <div className="detail-row">
                <span className="detail-key">Created</span>
                <span className="detail-val">{new Date(branch.created_at).toLocaleString()}</span>
              </div>
              {branch.expires_at && (
                <div className="detail-row">
                  <span className="detail-key">Expires</span>
                  <span className="detail-val">{new Date(branch.expires_at).toLocaleString()}</span>
                </div>
              )}
            </div>
          </div>
        </div>
      </td>
    </tr>
  )
}

// ── BranchRow ────────────────────────────────────────────────────────────────

interface BranchRowProps {
  branch: Branch
  selected: boolean
  onSelect: () => void
  onDelete: (name: string) => void
  metrics: BranchMetrics | null
}

function BranchRow({ branch, selected, onSelect, onDelete, metrics }: BranchRowProps) {
  const handleDelete = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (confirm(`Delete branch "${branch.name}"?`)) {
      onDelete(branch.name)
    }
  }

  return (
    <tr className={selected ? 'selected' : ''} onClick={onSelect}>
      <td><span className="branch-name">{branch.name}</span></td>
      <td><PhaseBadge phase={branch.status} /></td>
      <td>{relativeTime(branch.created_at)}</td>
      <td>{ttlLabel(branch)}</td>
      <td>
        {branch.port ? (
          <div className="port-cell">
            <span>{branch.port}</span>
            <CopyButton text={String(branch.port)} />
          </div>
        ) : <span style={{ color: 'var(--text-muted)' }}>—</span>}
      </td>
      <td>
        {branch.status === 'Ready' ? (
          metrics !== null ? (
            <span className="conn-count">{metrics.available ? metrics.threads_connected : '—'}</span>
          ) : (
            <span className="loading-text">...</span>
          )
        ) : <span style={{ color: 'var(--text-muted)' }}>—</span>}
      </td>
      <td>
        <button className="btn btn-danger btn-sm" onClick={handleDelete}>
          Delete
        </button>
      </td>
    </tr>
  )
}

// ── BranchesTab ──────────────────────────────────────────────────────────────

interface BranchesTabProps {
  branches: Branch[]
  stats: Stats | null
  onRefresh: () => void
  onCreate: (name: string, snapshotRef: string, ttlHours: number, dbType: string) => Promise<void>
  onDelete: (name: string) => Promise<void>
}

function BranchesTab({ branches, stats, onRefresh, onCreate, onDelete }: BranchesTabProps) {
  const [selectedName, setSelectedName] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [metrics, setMetrics] = useState<Record<string, BranchMetrics | null>>({})

  // Load metrics for Ready branches
  useEffect(() => {
    for (const b of branches) {
      if (b.status === 'Ready' && metrics[b.name] === undefined) {
        setMetrics(prev => ({ ...prev, [b.name]: null }))
        api.branches.getMetrics(b.name)
          .then(m => setMetrics(prev => ({ ...prev, [b.name]: m })))
          .catch(() => setMetrics(prev => ({ ...prev, [b.name]: { available: false, threads_connected: 0 } })))
      }
    }
  }, [branches, metrics])

  const selectedBranch = branches.find(b => b.name === selectedName) ?? null

  const handleSelect = (name: string) => {
    setSelectedName(prev => prev === name ? null : name)
  }

  const handleDelete = async (name: string) => {
    await onDelete(name)
    if (selectedName === name) setSelectedName(null)
  }

  return (
    <>
      {/* Stats bar */}
      {stats && (
        <div className="stats-bar">
          <StatCard label="Total" value={stats.total} variant="total" />
          <StatCard label="Ready" value={stats.ready} variant="ready" />
          <StatCard label="Creating" value={stats.creating} variant="creating" />
          <StatCard label="Error" value={stats.error} variant="error" />
          {stats.pending > 0 && <StatCard label="Pending" value={stats.pending} variant="pending" />}
          {stats.deleting > 0 && <StatCard label="Deleting" value={stats.deleting} variant="deleting" />}
        </div>
      )}

      {/* Toolbar */}
      <div className="toolbar">
        <button className="btn btn-primary" onClick={() => setShowCreate(true)}>
          + New Branch
        </button>
        <div className="spacer" />
        <button className="btn" onClick={onRefresh}>Refresh</button>
      </div>

      {/* Table */}
      <div className="branch-table-wrap">
        {branches.length === 0 ? (
          <div className="empty-state">
            <h3>No branches</h3>
            <p>Create a branch to get started</p>
          </div>
        ) : (
          <table className="branch-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Phase</th>
                <th>Age</th>
                <th>TTL</th>
                <th>NodePort</th>
                <th>Connections</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {branches.map(b => (
                <>
                  <BranchRow
                    key={b.name}
                    branch={b}
                    selected={selectedName === b.name}
                    onSelect={() => handleSelect(b.name)}
                    onDelete={handleDelete}
                    metrics={metrics[b.name] ?? null}
                  />
                  {selectedName === b.name && selectedBranch && (
                    <DetailPanel key={`detail-${b.name}`} branch={selectedBranch} />
                  )}
                </>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {showCreate && (
        <CreateModal
          onClose={() => setShowCreate(false)}
          onCreate={onCreate}
        />
      )}
    </>
  )
}

// ── SnapshotsTab ─────────────────────────────────────────────────────────────

function SnapshotsTab() {
  const [snapshots, setSnapshots] = useState<Snapshot[] | null>(null)
  const [notConfigured, setNotConfigured] = useState(false)
  const [taking, setTaking] = useState(false)
  const [err, setErr] = useState('')
  const [dbTypeFilter, setDbTypeFilter] = useState('mysql')

  const load = useCallback(() => {
    api.snapshots.list(dbTypeFilter)
      .then(snaps => { setSnapshots(snaps); setNotConfigured(false) })
      .catch(e => {
        if (String(e).includes('501')) setNotConfigured(true)
        else setErr(String(e))
      })
  }, [dbTypeFilter])

  useEffect(() => { load() }, [load])

  const handleTake = async () => {
    setTaking(true)
    setErr('')
    try {
      await api.snapshots.take(dbTypeFilter)
      load()
    } catch (ex) {
      setErr(String(ex))
    } finally {
      setTaking(false)
    }
  }

  if (notConfigured) {
    return (
      <div className="info-banner">
        VolumeProvider not configured — snapshot operations are unavailable.
        Set <code>ZFSDB_ZFSAGENT_URL</code> to enable.
      </div>
    )
  }

  return (
    <>
      {err && <div className="error-banner">{err}</div>}
      <div className="toolbar">
        <select
          value={dbTypeFilter}
          onChange={e => setDbTypeFilter(e.target.value)}
          className="db-type-filter"
        >
          <option value="mysql">MySQL</option>
          <option value="postgres">PostgreSQL</option>
          <option value="redis">Redis</option>
        </select>
        <button className="btn btn-primary" onClick={handleTake} disabled={taking}>
          {taking ? 'Taking...' : 'Take Snapshot'}
        </button>
        <div className="spacer" />
        <button className="btn" onClick={load}>Refresh</button>
      </div>
      <div className="snapshots-panel">
        {!snapshots ? (
          <div className="empty-state"><p>Loading...</p></div>
        ) : snapshots.length === 0 ? (
          <div className="empty-state">
            <h3>No snapshots</h3>
            <p>Take a snapshot to get started</p>
          </div>
        ) : (
          <table className="snapshots-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>DB Type</th>
                <th>Created At</th>
              </tr>
            </thead>
            <tbody>
              {snapshots.map(s => (
                <tr key={s.name}>
                  <td>{s.name}</td>
                  <td>{s.database_type ?? '—'}</td>
                  <td>{new Date(s.created_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  )
}

// ── App ───────────────────────────────────────────────────────────────────────

type Tab = 'branches' | 'snapshots'

export default function App() {
  const [tab, setTab] = useState<Tab>('branches')
  const [branches, setBranches] = useState<Branch[]>([])
  const [stats, setStats] = useState<Stats | null>(null)
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)
  const [err, setErr] = useState('')
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const loadBranches = useCallback(async () => {
    try {
      const [bs, st] = await Promise.all([
        api.branches.list(),
        api.stats.get(),
      ])
      setBranches(bs)
      setStats(st)
      setLastUpdated(new Date())
      setErr('')
    } catch (ex) {
      setErr(String(ex))
    }
  }, [])

  // Auto-polling
  useEffect(() => {
    loadBranches()
  }, [loadBranches])

  useEffect(() => {
    const interval = hasInProgress(branches) ? 5000 : 10000
    if (intervalRef.current) clearInterval(intervalRef.current)
    intervalRef.current = setInterval(loadBranches, interval)
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current)
    }
  }, [branches, loadBranches])

  const handleCreate = async (name: string, snapshotRef: string, ttlHours: number, dbType: string) => {
    await api.branches.create({
      name,
      snapshot_ref: snapshotRef || undefined,
      ttl_hours: ttlHours || undefined,
      database_type: dbType || undefined,
    })
    await loadBranches()
  }

  const handleDelete = async (name: string) => {
    await api.branches.delete(name)
    await loadBranches()
  }

  return (
    <div className="layout">
      <header className="header">
        <h1>BranchDB Admin</h1>
        <nav className="nav">
          <button className={tab === 'branches' ? 'active' : ''} onClick={() => setTab('branches')}>
            Branches
          </button>
          <button className={tab === 'snapshots' ? 'active' : ''} onClick={() => setTab('snapshots')}>
            Snapshots
          </button>
        </nav>
        <div className="header-right">
          <div className="refresh-dot" />
          {lastUpdated && <span>Updated {relativeTime(lastUpdated.toISOString())}</span>}
        </div>
      </header>

      <main className="main">
        {err && <div className="error-banner">{err}</div>}

        {tab === 'branches' && (
          <BranchesTab
            branches={branches}
            stats={stats}
            onRefresh={loadBranches}
            onCreate={handleCreate}
            onDelete={handleDelete}
          />
        )}
        {tab === 'snapshots' && <SnapshotsTab />}
      </main>
    </div>
  )
}
