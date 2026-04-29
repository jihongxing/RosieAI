#!/usr/bin/env bash
set -euo pipefail

PUBLIC_HOST="${1:-}"
if [[ -z "$PUBLIC_HOST" ]]; then
  echo "Usage: $0 <public-ip-or-domain>"
  echo "Example: $0 119.28.50.29"
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="$ROOT_DIR/services/call-webhook/.env"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$ROOT_DIR/services/call-webhook/.env.example" "$ENV_FILE"
fi

python3 - "$ENV_FILE" "$PUBLIC_HOST" <<'PY'
from pathlib import Path
import sys

env_path = Path(sys.argv[1])
public_host = sys.argv[2]

updates = {
    "ROSIE_USE_AI_GREETING": "true",
    "ROSIE_AI_AGENT_URL": "http://172.17.0.1:8010",
    "ROSIE_AI_TIMEOUT_SECONDS": "10",
    "ROSIE_REALTIME_LISTEN_ENABLED": "true",
    "ROSIE_REALTIME_WS_URL": f"ws://{public_host}:8020/ws/jambonz/audio",
    "ROSIE_REALTIME_ACTION_HOOK": f"http://{public_host}:8000/webhooks/jambonz/listen-complete",
}

lines = env_path.read_text(encoding="utf-8").splitlines()
seen = set()
new_lines = []

for line in lines:
    if not line or line.lstrip().startswith("#") or "=" not in line:
        new_lines.append(line)
        continue
    key = line.split("=", 1)[0]
    if key in updates:
        new_lines.append(f"{key}={updates[key]}")
        seen.add(key)
    else:
        new_lines.append(line)

for key, value in updates.items():
    if key not in seen:
        new_lines.append(f"{key}={value}")

env_path.write_text("\n".join(new_lines) + "\n", encoding="utf-8")
print(f"Updated {env_path}")
PY

echo
echo "Current realtime settings:"
grep -E 'ROSIE_USE_AI_GREETING|ROSIE_AI_AGENT_URL|ROSIE_REALTIME' "$ENV_FILE"
echo
echo "Restart call-webhook:"
echo "  cd $ROOT_DIR/services/call-webhook && docker compose up -d --build"

