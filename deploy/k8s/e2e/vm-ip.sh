#!/bin/bash
# vm-ip.sh — Colima VM の接続先アドレスを解決する。
#
# 使い方: vm-ip.sh <profile> <vm|host>
#   vm:   VM 内部（Pod / zfsagent）から届くアドレス。
#         ホスト到達可能な専用 IP（--network-address）があればそれ、無ければ VM 内部 IP。
#   host: ホスト（E2E テストランナー）から届くアドレス。
#         専用 IP が無ければ 127.0.0.1（lima がリッスンポートを自動フォワードする）。
#
# colima list -j はバージョンにより JSON 配列 / NDJSON、キーも profile / name と
# 揺れがあるため両対応でパースする。
set -euo pipefail

profile=${1:?usage: vm-ip.sh <profile> <vm|host>}
mode=${2:-vm}

addr=$(colima list -j 2>/dev/null | python3 -c "
import sys, json
lines = [l for l in sys.stdin if l.strip()]
try:
    items = [json.loads(l) for l in lines]
except json.JSONDecodeError:
    items = json.loads(''.join(lines))
if len(items) == 1 and isinstance(items[0], list):
    items = items[0]
print(next((x.get('address') or x.get('ip_address') or ''
            for x in items
            if (x.get('profile') or x.get('name')) == '$profile'), ''))
" 2>/dev/null || true)

if [ -n "$addr" ]; then
  echo "$addr"
elif [ "$mode" = "host" ]; then
  echo "127.0.0.1"
else
  colima ssh --profile "$profile" -- hostname -I 2>/dev/null | awk '{print $1; exit}'
fi
