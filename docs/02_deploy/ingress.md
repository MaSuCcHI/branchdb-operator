# Ingress での外部公開

API サーバーを HTTPS で外部公開する方法を説明します。

## Nginx Ingress Controller

### 前提

```bash
# Nginx Ingress Controller のインストール（未インストールの場合）
helm upgrade --install ingress-nginx ingress-nginx \
  --repo https://kubernetes.github.io/ingress-nginx \
  --namespace ingress-nginx --create-namespace
```

### values.yaml の設定

```yaml
# values.yaml
apiServer:
  service:
    type: ClusterIP   # Ingress がルーティングするので ClusterIP
```

### Ingress リソースの作成

```yaml
# branchdb-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: branchdb
  namespace: branchdb-system
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
    # TLS 証明書（cert-manager を使う場合）
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

### 前提

AWS Load Balancer Controller がインストール済みであること。

### Ingress リソースの作成

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

## Traefik (k3s デフォルト)

k3s はデフォルトで Traefik が入っています。

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

## WebSocket のサポート

API サーバーは `/ws` に WebSocket エンドポイントを持ちます。  
Nginx Ingress では追加のアノテーションが必要です：

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
