#!/usr/bin/env bash
set -euo pipefail

PUBLIC_HOST="${1:-}"
if [[ -z "$PUBLIC_HOST" ]]; then
  PUBLIC_HOST="$(curl -fsS -4 ifconfig.me || true)"
fi

if [[ -z "$PUBLIC_HOST" ]]; then
  echo "Could not detect public host. Usage: $0 <public-ip-or-domain>"
  exit 1
fi

echo "Public host: $PUBLIC_HOST"
echo

echo "Local health:"
curl -fsS http://127.0.0.1:8000/health && echo
curl -fsS http://127.0.0.1:8020/health && echo

echo
echo "Public health:"
curl --connect-timeout 5 -fsS "http://$PUBLIC_HOST:8000/health" && echo
curl --connect-timeout 5 -fsS "http://$PUBLIC_HOST:8020/health" && echo

echo
echo "Webhook response:"
curl -fsS -X POST http://127.0.0.1:8000/webhooks/jambonz/call \
  -H "Content-Type: application/json" \
  -d "{\"callSid\":\"phase3-check\",\"from\":\"+8613811112222\",\"to\":\"+8617000000000\",\"direction\":\"inbound\",\"callStatus\":\"trying\"}" \
  && echo

echo
echo "Realtime sessions:"
curl -fsS http://127.0.0.1:8020/sessions && echo

