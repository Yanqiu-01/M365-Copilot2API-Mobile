#!/usr/bin/env bash
# 在没有 Android framework / 真机的环境中，对 APK 内的 ARM64 Go 子进程做最小冒烟。
# 用法：qemu-smoke.sh <apk> [port]
set -euo pipefail

APK_INPUT=${1:?用法: qemu-smoke.sh <apk> [port]}
PORT=${2:-4141}
APK=$(realpath "$APK_INPUT")
QEMU=${QEMU:-qemu-aarch64-static}
[ -f "$APK" ] || { echo "APK 不存在: $APK" >&2; exit 1; }
command -v "$QEMU" >/dev/null 2>&1 || { echo "找不到 QEMU: $QEMU" >&2; exit 1; }

WORK=$(mktemp -d /tmp/m365-qemu-smoke.XXXXXX)
ROOT="$WORK/root"
DATA="$WORK/data"
LOG="$WORK/server.log"
PIDFILE="$WORK/pid"
cleanup() {
  if [ -f "$PIDFILE" ]; then
    kill "$(cat "$PIDFILE")" 2>/dev/null || true
    wait "$(cat "$PIDFILE")" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT
mkdir -p "$ROOT/system/bin" "$ROOT/app/gateway/web" "$DATA"

for name in index.html login.html debug.html; do
  unzip -p "$APK" "assets/web/$name" > "$ROOT/app/gateway/web/$name"
done
unzip -p "$APK" lib/arm64-v8a/libm365.so > "$ROOT/app/gateway/libm365.so"
chmod 700 "$ROOT/app/gateway/libm365.so"
# qemu-user 通过 -L 模拟 Android 的动态链接器路径；这是链接器入口，
# 待测程序仍然是从最终 APK 提取出来的 libm365.so。
ln -sf /lib/ld-linux-aarch64.so.1 "$ROOT/system/bin/linker64"

(
  cd "$ROOT/app/gateway"
  M365_LISTEN="127.0.0.1:$PORT" \
  M365_DATA_DIR="$DATA" \
  M365_ADMIN_PASSWORD='qemu-test-password' \
  M365_AUTO_CLEANUP=0 \
  "$QEMU" -L "$ROOT" "$ROOT/app/gateway/libm365.so" >"$LOG" 2>&1
) &
echo $! > "$PIDFILE"
BASE="http://127.0.0.1:$PORT"
for _ in $(seq 1 80); do
  code=$(curl -sS --max-time 1 -o /dev/null -w '%{http_code}' "$BASE/" 2>/dev/null || true)
  if [ "$code" = 200 ]; then break; fi
  if ! kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    echo 'QEMU 子进程提前退出；日志：' >&2
    cat "$LOG" >&2
    exit 1
  fi
  sleep 0.25
done
code=$(curl -sS --max-time 3 -o /dev/null -w '%{http_code}' "$BASE/" 2>/dev/null || true)
[ "$code" = 200 ] || { echo "GET / 返回 $code" >&2; cat "$LOG" >&2; exit 1; }
code=$(curl -sS --max-time 3 -o /dev/null -w '%{http_code}' "$BASE/login" 2>/dev/null || true)
[ "$code" = 200 ] || { echo "GET /login 返回 $code" >&2; cat "$LOG" >&2; exit 1; }
COOKIE="$WORK/cookie.txt"
code=$(curl -sS --max-time 3 -c "$COOKIE" -b "$COOKIE" \
  -H 'Content-Type: application/json' \
  -d '{"password":"qemu-test-password"}' \
  -o /dev/null -w '%{http_code}' "$BASE/api/admin/login" 2>/dev/null || true)
[ "$code" = 200 ] || { echo "POST /api/admin/login 返回 $code" >&2; cat "$LOG" >&2; exit 1; }
code=$(curl -sS --max-time 3 -c "$COOKIE" -b "$COOKIE" \
  -o /dev/null -w '%{http_code}' "$BASE/api/health" 2>/dev/null || true)
[ "$code" = 200 ] || { echo "GET /api/health 返回 $code" >&2; cat "$LOG" >&2; exit 1; }

MODELS="$WORK/models.json"
code=$(curl -sS --max-time 5 -c "$COOKIE" -b "$COOKIE" \
  -o "$MODELS" -w '%{http_code}' "$BASE/api/admin/models" 2>/dev/null || true)
[ "$code" = 200 ] || { echo "GET /api/admin/models 返回 $code" >&2; cat "$LOG" >&2; exit 1; }
python3 - "$MODELS" <<'PY'
import json, sys
payload = json.load(open(sys.argv[1], encoding='utf-8'))
want = [
    'gpt-5.2', 'gpt-5.2-reasoning', 'gpt-5.3', 'gpt-5.3-reasoning',
    'gpt-5.4', 'gpt-5.4-reasoning', 'gpt-5.5', 'gpt-5.5-reasoning',
    'gpt-5.6-reasoning', 'claude-sonnet', 'claude-sonnet-reasoning',
    'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna',
]
data = payload.get('data')
if payload.get('object') != 'list' or not isinstance(data, list):
    raise SystemExit(f'模型目录结构错误: {payload!r}')
ids = [model.get('id') for model in data]
if ids != want:
    raise SystemExit(f'模型目录错误: {ids!r}, want {want!r}')
if any(str(model_id).lower().endswith('-chat') for model_id in ids):
    raise SystemExit(f'内部 tone 泄漏为公开模型名: {ids!r}')
if any('configured' in model for model in data):
    raise SystemExit('模型目录不应包含 recovered-only configured 字段')
PY

SETTINGS="$WORK/settings.json"
code=$(curl -sS --max-time 5 -c "$COOKIE" -b "$COOKIE" \
  -o "$SETTINGS" -w '%{http_code}' "$BASE/api/admin/settings" 2>/dev/null || true)
[ "$code" = 200 ] || { echo "GET /api/admin/settings 返回 $code" >&2; cat "$LOG" >&2; exit 1; }
python3 - "$SETTINGS" <<'PY'
import json, sys
payload = json.load(open(sys.argv[1], encoding='utf-8'))
settings = payload.get('settings', {})
want = {
    'maxToolCallsPerTurn': 32,
    'maxToolRounds': 512,
    'contextWindow': 262144,
    'maxOutputTokens': 16384,
    'chatTimeoutSeconds': 600,
    'imageTimeoutSeconds': 180,
}
for key, value in want.items():
    if settings.get(key) != value:
        raise SystemExit(f'设置 {key}={settings.get(key)!r}, want {value!r}')
want_tones = [
    'Gpt_5_2_Chat', 'Gpt_5_2_Reasoning', 'Gpt_5_3_Chat', 'Gpt_5_3_Reasoning',
    'Gpt_5_4_Chat', 'Gpt_5_4_Reasoning', 'Gpt_5_5_Chat', 'Gpt_5_5_Reasoning',
    'Gpt_5_6_Reasoning', 'Claude_Sonnet', 'Claude_Sonnet_Reasoning',
]
if payload.get('upstreamTones') != want_tones:
    raise SystemExit(f'upstreamTones={payload.get("upstreamTones")!r}')
PY

code=$(curl -sS --max-time 3 -c "$COOKIE" -b "$COOKIE" \
  -X POST -o /dev/null -w '%{http_code}' "$BASE/api/admin/models/sync" 2>/dev/null || true)
[ "$code" = 404 ] || { echo "POST /api/admin/models/sync 返回 $code，原 APK 不存在该入口" >&2; cat "$LOG" >&2; exit 1; }
printf 'QEMU smoke OK: APK=%s; /=%s /login=%s login=%s health=%s models=%s settings=%s sync=%s\n' \
  "$(basename "$APK")" 200 200 200 200 200 200 404
