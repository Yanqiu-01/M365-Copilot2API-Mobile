#!/usr/bin/env bash
# 把原 APK 改包名并重签名，产出可与原版并存的 v2。
#
# 依赖：apktool、aapt2、zipalign、apksigner、keytool
# 注意：apktool b 必须加 --use-aapt2 —— 旧版 aapt 处理本资源包会崩溃
#       （brut.common.BrutException + "First type is not attr!"，exit 134）
set -euo pipefail

SRC=${1:?用法: repack-v2.sh <原始 base.apk> [输出目录]}
OUT=${2:-./out}
OLD_PKG=com.m365.gateway
NEW_PKG=com.m365.gateway2
NEW_LABEL='M365 网关 v2'
KS="$OUT/m365-gateway-v2.jks"
KS_PASS=[REDACTED]
KS_ALIAS=m365v2

mkdir -p "$OUT"
rm -rf "$OUT/work"
apktool d -f -o "$OUT/work" "$SRC"

cd "$OUT/work"

# 1) manifest：包名 + provider authority
sed -i "s|package=\"$OLD_PKG\"|package=\"$NEW_PKG\"|" AndroidManifest.xml
sed -i "s|\"$OLD_PKG\.wake\"|\"$NEW_PKG.wake\"|g" AndroidManifest.xml

# 2) 应用名
sed -i "s|<string name=\"app_name\">[^<]*</string>|<string name=\"app_name\">$NEW_LABEL</string>|" \
  res/values/strings.xml

# 3) 应用内广播 action（避免两版互相唤醒）
for a in KEEPALIVE START STOP TUNNEL_START; do
  grep -rl "\"$OLD_PKG\.$a\"" smali/ 2>/dev/null | while read -r f; do
    sed -i "s|\"$OLD_PKG\.$a\"|\"$NEW_PKG.$a\"|g" "$f"
  done
  sed -i "s|\"$OLD_PKG\.$a\"|\"$NEW_PKG.$a\"|g" AndroidManifest.xml
done

# smali 类路径不改：Android 按 manifest package 识别身份

cd - >/dev/null

# 4) 打包（必须用 aapt2）
apktool b "$OUT/work" --use-aapt2 -o "$OUT/unsigned.apk"

# 5) 对齐
zipalign -p -f 4 "$OUT/unsigned.apk" "$OUT/aligned.apk"
zipalign -c 4 "$OUT/aligned.apk"

# 6) 密钥（不存在则生成）
if [ ! -f "$KS" ]; then
  keytool -genkeypair -v -keystore "$KS" -alias "$KS_ALIAS" \
    -keyalg RSA -keysize 4096 -validity 10950 \
    -storepass "$KS_PASS" -keypass "$KS_PASS" \
    -dname "CN=M365 Gateway v2, OU=Recovery, O=Self-Signed, C=CN"
  echo "已生成新密钥库：$KS —— 请务必备份"
fi

# 7) 签名
apksigner sign --ks "$KS" --ks-key-alias "$KS_ALIAS" \
  --ks-pass "pass:$KS_PASS" --key-pass "pass:$KS_PASS" \
  --v1-signing-enabled true --v2-signing-enabled true --v3-signing-enabled true \
  --out "$OUT/M365-Gateway-v2.apk" "$OUT/aligned.apk"

# 8) 验证
apksigner verify --print-certs "$OUT/M365-Gateway-v2.apk" | head -12
aapt dump badging "$OUT/M365-Gateway-v2.apk" | grep -E "^package|application-label|native-code"

sha256sum "$OUT/M365-Gateway-v2.apk" | tee "$OUT/M365-Gateway-v2.apk.sha256"
echo "完成：$OUT/M365-Gateway-v2.apk"
