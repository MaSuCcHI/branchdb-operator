# Expose via Ingress

This page explains how to expose the API server externally over HTTPS.

## Nginx Ingress Controller

### Prerequisites

```bash
# Install the Nginx Ingress Controller (if not already installed)
helm upgrade --install ingress-nginx ingress-nginx \
  --repo https://kubernetes.github.io/ingress-nginx \
  --namespace ingress-nginx --create-namespace
```

### values.yaml Configuration

```yaml
# values.yaml
apiServer:
  service:
    type: ClusterIP   # ClusterIP because the Ingress handles routing
```

### Create the Ingress Resource

```yaml
# branchdb-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: branchdb
  namespace: branchdb-system
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
    # TLS certificate (when using cert-manager)
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - branchdb.example.com
      secretName: branchdb-tls
  rules:
    - host: branchdb.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: branchdb-api
                port:
                  number: 8080
```

```bash
kubectl apply -f branchdb-ingress.yaml
```

---

## AWS ALB Ingress (EKS)

### Prerequisites

The AWS Load Balancer Controller must already be installed.

### Create the Ingress Resource

```yaml
# branchdb-alb-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: branchdb
  namespace: branchdb-system
  annotations:
    kubernetes.io/ingress.class: alb
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/target-type: ip
    alb.ingress.kubernetes.io/certificate-arn: arn:aws:acm:<region>:<account>:certificate/<id>
    alb.ingress.kubernetes.io/listen-ports: '[{"HTTPS":443}]'
spec:
  rules:
    - host: branchdb.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: branchdb-api
                port:
                  number: 8080
```

---

## Traefik (k3s default)

k3s ships with Traefik by default.

```yaml
# branchdb-traefik-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: branchdb
  namespace: branchdb-system
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
    traefik.ingress.kubernetes.io/router.tls: "true"
    traefik.ingress.kubernetes.io/router.tls.certresolver: myresolver
spec:
  rules:
    - host: branchdb.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: branchdb-api
                port:
                  number: 8080
```

---

## WebSocket Support

The API server has a WebSocket endpoint at `/ws`.
The Nginx Ingress requires additional annotations:

```yaml
metadata:
  annotations:
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
    nginx.ingress.kubernetes.io/server-snippets: |
      location /ws {
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
      }
```
