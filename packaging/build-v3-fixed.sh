#!/usr/bin/env bash
# 用恢复的源码编译 libm365.so，替换进 APK，产出含修复的 v3。
#
# 关键事实：libm365.so 不是 JNI 库，而是被 GatewayService 以
# ProcessBuilder 启动的 PIE 可执行文件（只导出 main.main、
# INTERP=/system/bin/linker64）。恢复的 cmd/server 正是同一形态。
#
# 必须用 -buildmode=pie。-buildmode=exe 会报
#   runtime.gcdata: missing Go type information for global symbol .dynsym
set -euo pipefail

SRC=${1:?用法: build-v3-fixed.sh <原始 base.apk> [输出目录]}
OUT=${2:-./out-v3}
REPO=$(cd "$(dirname "$0")/.." && pwd)
NEW_PKG=com.m365.gateway3
NEW_LABEL='M365 网关 v3 修复版'
KS=${KS:-$OUT/m365-gateway-v2.jks}
KS_PASS=${KS_PASS:-[REDACTED]}
KS_ALIAS=${KS_ALIAS:-m365v2}

mkdir -p "$OUT"

echo "== 1/5 交叉编译 libm365.so =="
( cd "$REPO" && CGO_ENABLED=0 GOOS=android GOARCH=arm64 GOARM64=v8.0 \
    go build -trimpath -buildmode=pie -o "$OUT/libm365.so" ./cmd/server )
readelf -h "$OUT/libm365.so" | grep -E 'Type|Entry point'
readelf -p .interp "$OUT/libm365.so" | grep -oE '/[a-z/0-9._]+'

echo "== 2/5 反编译 APK =="
rm -rf "$OUT/work"
apktool d -f -o "$OUT/work" "$SRC"

echo "== 3/5 替换 .so、同步 web 资源、改包名 =="
cp "$OUT/libm365.so" "$OUT/work/lib/arm64-v8a/libm365.so"
for f in index.html login.html debug.html; do
  [ -f "$REPO/web/$f" ] && cp "$REPO/web/$f" "$OUT/work/assets/web/$f"
done

cd "$OUT/work"
sed -i "s|package=\"com\.m365\.gateway\"|package=\"$NEW_PKG\"|" AndroidManifest.xml
sed -i "s|\"com\.m365\.gateway\.wake\"|\"$NEW_PKG.wake\"|g" AndroidManifest.xml
sed -i "s|<string name=\"app_name\">[^<]*</string>|<string name=\"app_name\">$NEW_LABEL</string>|" \
  res/values/strings.xml
for a in KEEPALIVE START STOP TUNNEL_START; do
  grep -rl "\"com\.m365\.gateway\.$a\"" smali/ 2>/dev/null | while read -r f; do
    sed -i "s|\"com\.m365\.gateway\.$a\"|\"$NEW_PKG.$a\"|g" "$f"
  done
  sed -i "s|\"com\.m365\.gateway\.$a\"|\"$NEW_PKG.$a\"|g" AndroidManifest.xml
done
cd - >/dev/null

echo "== 4/5 打包、对齐、签名 =="
apktool b "$OUT/work" --use-aapt2 -o "$OUT/unsigned.apk"   # 必须 --use-aapt2
zipalign -p -f 4 "$OUT/unsigned.apk" "$OUT/aligned.apk"
zipalign -c 4 "$OUT/aligned.apk" >/dev/null

if [ ! -f "$KS" ]; then
  keytool -genkeypair -v -keystore "$KS" -alias "$KS_ALIAS" \
    -keyalg RSA -keysize 4096 -validity 10950 \
    -storepass "$KS_PASS" -keypass "$KS_PASS" \
    -dname "CN=M365 Gateway v2, OU=Recovery, O=Self-Signed, C=CN"
  echo "已生成密钥库 $KS —— 请备份"
fi
apksigner sign --ks "$KS" --ks-key-alias "$KS_ALIAS" \
  --ks-pass "pass:$KS_PASS" --key-pass "pass:$KS_PASS" \
  --v1-signing-enabled true --v2-signing-enabled true --v3-signing-enabled true \
  --out "$OUT/M365-Gateway-v3-fixed.apk" "$OUT/aligned.apk"

echo "== 5/5 验证 =="
apksigner verify "$OUT/M365-Gateway-v3-fixed.apk"
aapt dump badging "$OUT/M365-Gateway-v3-fixed.apk" | grep -E "^package|application-label|native-code"
python3 - "$SRC" "$OUT/M365-Gateway-v3-fixed.apk" <<'PY'
import sys,zipfile,hashlib
o,n=(zipfile.ZipFile(p) for p in sys.argv[1:3])
f=lambda z,x: hashlib.sha256(z.read(x)).hexdigest()
so='lib/arm64-v8a/libm365.so'
print("libm365.so 已替换 :", f(o,so)!=f(n,so))
cf='lib/arm64-v8a/libcloudflared.so'
print("libcloudflared 未动:", f(o,cf)==f(n,cf))
print("条目数            :", len(o.namelist()), "->", len(n.namelist()))
PY
sha256sum "$OUT/M365-Gateway-v3-fixed.apk" | tee "$OUT/M365-Gateway-v3-fixed.apk.sha256"
