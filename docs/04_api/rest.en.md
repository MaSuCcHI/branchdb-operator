# REST API Reference

Base URL: `http://<api-server>:8080`

All responses are JSON. Errors are in the form `{"error": "<message>"}`.

---

## Branch Operations

### POST /branches — Create a Branch

```
POST /branches
Content-Type: application/json
```

**Request**

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | ✅ | Branch name (only `[a-z0-9-]` allowed) |
| `snapshot_ref` | string | ✅ | Base snapshot name |
| `ttl_hours` | integer | | Lifetime (hours). `0` or omitted means no expiry. |
| `database_type` | string | | Database type. `mysql` (default) / `postgres` / `redis`. |
| `database_version` | string | | Image tag override. When omitted, the per-type default is used. |

**Response: 202 Accepted**

```json
{
  "name":         "feature-payment",
  "status":       "creating",
  "host":         "192.168.1.100",
  "port":         31234,
  "dsn":          "root@tcp(192.168.1.100:31234)/",
  "cluster_host": "",
  "cluster_port": 0,
  "snapshot_ref": "snap-20260101",
  "ttl_hours":    24,
  "created_at":   "2026-05-28T10:00:00Z",
  "expires_at":   "2026-05-29T10:00:00Z"
}
```

While `port: 0`, the NodePort has not yet been assigned.
After creating the CR, the API server polls for up to 5 seconds and returns the response once the NodePort is determined.

**Errors**

| Code | Condition |
|---|---|
| `400 Bad Request` | The request body is invalid, or `name` is empty |
| `500 Internal Server Error` | Connection error to the K8s API |

---

### GET /branches — List Branches

```
GET /branches
```

**Response: 200 OK** — an array of BranchResponse

```json
[
  {
    "name":         "feature-payment",
    "status":       "ready",
    "host":         "192.168.1.100",
    "port":         31234,
    "dsn":          "root@tcp(192.168.1.100:31234)/",
    "cluster_host": "feature-payment.branchdb-system.svc.cluster.local",
    "cluster_port": 3306,
    "snapshot_ref": "snap-20260101",
    "ttl_hours":    24,
    "created_at":   "2026-05-28T10:00:00Z",
    "expires_at":   "2026-05-29T10:00:00Z"
  }
]
```

---

### GET /branches/{name} — Get Branch Status

```
GET /branches/{name}
```

**Response: 200 OK**

BranchResponse (same structure as above)

| Field | Description |
|---|---|
| `status` | `pending` / `creating` / `ready` / `error` / `deleting` |
| `host` | Hostname for external connections (the value of `externalHost`) |
| `port` | NodePort (0 means not yet assigned) |
| `dsn` | `root@tcp(<host>:<port>)/` (set only when port > 0) |
| `cluster_host` | Internal cluster DNS (when connecting from a Pod in the same cluster) |
| `cluster_port` | Internal cluster port (always `3306`) |
| `message` | Contains a detailed message when `status: error` |

**Errors**

| Code | Condition |
|---|---|
| `404 Not Found` | The branch does not exist |
| `500 Internal Server Error` | K8s API error (other than Not Found) |

---

### DELETE /branches/{name} — Delete a Branch

```
DELETE /branches/{name}
```

**Response: 204 No Content**

Marks the `DatabaseBranch` CR for deletion. The actual resources (Pod/PVC/ZFS clone) are
cleaned up asynchronously by the Operator.

**Errors**

| Code | Condition |
|---|---|
| `404 Not Found` | The branch does not exist |

---

## Branch Detail Information

### GET /branches/{name}/pod — Get Pod Status

```
GET /branches/{name}/pod
```

Returns the status of the MySQL Pod (`branchdb-mysql-<name>`).

**Response: 200 OK**

```json
{
  "phase":   "Running",
  "ready":   true,
  "message": ""
}
```

| Field | Description |
|---|---|
| `phase` | Pod phase (`Pending` / `Running` / `Succeeded` / `Failed` / `Unknown`) |
| `ready` | Whether the Pod's Ready condition is True |
| `message` | Detailed message on error |

**Errors**

| Code | Condition |
|---|---|
| `503 Service Unavailable` | The Pod does not exist |

---

### GET /branches/{name}/metrics — Get MySQL Metrics

```
GET /branches/{name}/metrics
```

Connects directly to the branch MySQL and retrieves `Threads_connected`.

**Response: 200 OK** (when the MySQL connection succeeds)

```json
{
  "threads_connected": 3,
  "available":         true
}
```

**Response: 200 OK** (when the MySQL connection fails)

```json
{
  "threads_connected": 0,
  "available":         false,
  "error":             "dial tcp 192.168.1.100:3306: connect: connection refused"
}
```

> **Note:** The HTTP status is always `200`. Determine errors via `available: false`.

---

## Statistics

### GET /stats — Per-phase Counts

```
GET /stats
```

**Response: 200 OK**

```json
{
  "total":    10,
  "ready":    7,
  "creating": 2,
  "error":    1,
  "pending":  0,
  "deleting": 0
}
```

---

## Snapshots

> Enabled only when `ZFSDB_ZFSAGENT_URL` is set.
> When not set, returns `501 Not Implemented`.

### GET /snapshots — List Snapshots

```
GET /snapshots
```

**Response: 200 OK**

```json
[
  {
    "name":       "snap-20260101",
    "created_at": "2026-01-01T00:00:00Z"
  }
]
```

---

### POST /snapshots — Take a Snapshot Immediately

```
POST /snapshots
```

**Response: 201 Created**

```json
{
  "name":       "snap-20260528T120000",
  "created_at": "2026-05-28T12:00:00Z"
}
```

---

## Health Check

### GET /health

```
GET /health
```

**Response: 200 OK**

```json
{"status": "ok"}
```

---

## WebSocket

### GET /ws — Real-time Event Stream

A WebSocket connection lets you receive branch state changes in real time.

```javascript
// In production, terminate TLS at the Ingress and use a wss:// (secure) URL.
// For local development against the plain :8080 listener, use the insecure scheme.
const ws = new WebSocket('wss://<api-server>/ws');
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  console.log(msg.type, msg.data);
};
```

**Event format**

```json
{
  "type": "branch_updated",
  "data": {
    "name":   "feature-payment",
    "status": "ready",
    "port":   31234
  }
}
```

| type | Timing |
|---|---|
| `branch_updated` | When a branch's status changes (e.g. Creating → Ready) |
| `branch_deleted` | When a branch is deleted |

---

## Web Console

### GET /

Returns the browser-facing management UI (SPA).
Accessible on the same host and port as the API above.

```
http://<api-server>:8080/
```
