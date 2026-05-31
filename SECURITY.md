# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest (main) | ✅ |

## Reporting a Vulnerability

**Do not report security vulnerabilities through public GitHub issues.**

Please use [GitHub Security Advisories](https://github.com/MaSuCcHI/branchdb-operator/security/advisories/new) to report vulnerabilities privately.

Include as much of the following information as possible:

- Type of vulnerability (e.g., injection, authentication bypass, privilege escalation)
- Affected component (Operator, ZFS Agent, API server, Helm chart)
- Step-by-step instructions to reproduce
- Potential impact and severity estimate

You will receive a response within **5 business days**. We aim to release a patch within **30 days** of confirmation.

## Security Considerations

### ZFS Agent Token

The ZFS Agent uses a bearer token for authentication. Always:

- Set a strong, randomly generated token via `ZFSAGENT_TOKEN` (minimum 32 characters)
- Never expose the ZFS Agent port (default `:9090`) to untrusted networks
- Rotate the token using the `zfsAgent.token` Helm value and rolling restart

### Network Exposure

- The API server (`/branches`, `/snapshots`, etc.) has **no built-in authentication** in the OSS version
- Place an authenticating reverse proxy or Ingress controller in front of it in shared environments
- The ZFS Agent should be reachable only from the Kubernetes cluster (not from the internet)

### Principle of Least Privilege

The Operator requires RBAC permissions to create/delete Pods, PVCs, and Services in the branch namespace. Review `deploy/helm/branchdb/templates/rbac.yaml` and restrict to the minimum required namespaces.
