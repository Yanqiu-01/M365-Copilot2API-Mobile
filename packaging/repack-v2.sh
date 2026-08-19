#!/usr/bin/env bash
# 将原 APK 改为 com.m365.gateway2 并重签名，产出可与原版并存的 v2。
#
# 这不是代码修复版：libm365.so 和其他业务资源保持原 APK 内容。但 manifest
# package 改名后必须把相对组件名改为 DEX 中真实存在的完整类名，否则 Android
# 会去加载不存在的 com.m365.gateway2.MainActivity，应用会在点击时崩溃。
#
# 依赖：apktool、aapt2、zipalign、apksigner、keytool、python3
set -euo pipefail

SRC_INPUT=${1:?用法: repack-v2.sh <原始 base.apk> [输出目录]}
OUT_INPUT=${2:-./out-v2}
SRC=$(realpath "$SRC_INPUT")
OUT=$(realpath -m "$OUT_INPUT")
if [ ! -f "$SRC" ]; then
  echo "原始 APK 不存在: $SRC" >&2
  exit 1
fi

OLD_PKG=com.m365.gateway
NEW_PKG=com.m365.gateway2
NEW_LABEL='M365 网关 v2'
# 覆盖此前错误的 v2（2.24.0 / 59），但不影响原版或 v3。
VERSION_CODE=60
VERSION_NAME=2.24.1
KS=${KS:-$OUT/m365-gateway-v2.jks}
KS_PASS=${KS_PASS:-[REDACTED]}
KS_ALIAS=${KS_ALIAS:-m365v2}

mkdir -p "$OUT"

printf '%s\n' '== 1/5 反编译 APK =='
rm -rf "$OUT/work"
apktool d -f -o "$OUT/work" "$SRC"

printf '%s\n' '== 2/5 改包名、组件与应用私有标识 =='
python3 - "$OUT/work" "$OLD_PKG" "$NEW_PKG" "$NEW_LABEL" "$VERSION_CODE" "$VERSION_NAME" <<'PY'
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
old_pkg, new_pkg, label = sys.argv[2:5]
version_code, version_name = sys.argv[5:7]
components = (
    'MainActivity', 'AuthActivity', 'DiagActivity', 'TunnelActivity',
    'WakeProvider', 'GatewayService', 'KeepAliveReceiver', 'BootReceiver',
)
actions = ('KEEPALIVE', 'START', 'STOP', 'TUNNEL_START')

manifest_path = root / 'AndroidManifest.xml'
manifest = manifest_path.read_text(encoding='utf-8')
if f'package="{old_pkg}"' not in manifest:
    raise SystemExit(f'expected package not found: {old_pkg}')
manifest = manifest.replace(f'package="{old_pkg}"', f'package="{new_pkg}"', 1)

# Component class descriptors in classes.dex remain com.m365.gateway.*. A
# relative name would resolve against new_pkg and fail before MainActivity starts.
for component in components:
    manifest = manifest.replace(
        f'android:name=".{component}"',
        f'android:name="{old_pkg}.{component}"',
    )

# These are application-private identity strings, not Java class names.
manifest = manifest.replace(f'"{old_pkg}.wake"', f'"{new_pkg}.wake"')
for action in actions:
    manifest = manifest.replace(f'"{old_pkg}.{action}"', f'"{new_pkg}.{action}"')
manifest_path.write_text(manifest, encoding='utf-8')

strings_path = root / 'res/values/strings.xml'
strings_xml = strings_path.read_text(encoding='utf-8')
strings_xml = re.sub(
    r'<string name="app_name">[^<]*</string>',
    f'<string name="app_name">{label}</string>',
    strings_xml,
    count=1,
)
strings_path.write_text(strings_xml, encoding='utf-8')

yml_path = root / 'apktool.yml'
yml = yml_path.read_text(encoding='utf-8')
yml = re.sub(r"versionCode: '[^']*'", f"versionCode: '{version_code}'", yml, count=1)
yml = re.sub(r'versionName: .*', f'versionName: {version_name}', yml, count=1)
yml_path.write_text(yml, encoding='utf-8')

smali_dirs = [path for path in root.glob('smali*') if path.is_dir()]
for smali_dir in smali_dirs:
    for path in smali_dir.rglob('*.smali'):
        text = path.read_text(encoding='utf-8')
        updated = text.replace(f'{old_pkg}.wake', f'{new_pkg}.wake')
        for action in actions:
            updated = updated.replace(f'{old_pkg}.{action}', f'{new_pkg}.{action}')
        if updated != text:
            path.write_text(updated, encoding='utf-8')

# Fail early if a future APK changes the Java class layout or introduces a new
# relative component that would again resolve into the renamed package.
for component in components:
    suffix = old_pkg.replace('.', '/') + '/' + component + '.smali'
    if not any((smali_dir / suffix).is_file() for smali_dir in smali_dirs):
        raise SystemExit(f'component class missing from DEX/smali: {suffix}')
if re.search(r'android:name="\.(?:' + '|'.join(components) + r')"', manifest):
    raise SystemExit('relative application component remains in manifest')
for token in (f'{old_pkg}.wake', *(f'{old_pkg}.{action}' for action in actions)):
    if token in manifest:
        raise SystemExit(f'old private identity remains in manifest: {token}')
PY

printf '%s\n' '== 3/5 打包、恢复 native 执行权限并对齐 =='
apktool b "$OUT/work" --use-aapt2 -o "$OUT/unsigned.apk"

# apktool 重打包会清空 lib/*.so 的 ZIP Unix mode。GatewayService 会直接用
# ProcessBuilder 启动 libm365.so，因此将 native entries 恢复为原包的 0700。
python3 - "$OUT/unsigned.apk" "$OUT/unsigned-mode.apk" <<'PY'
import sys
import zipfile

source, target = sys.argv[1:3]
with zipfile.ZipFile(source, 'r') as zin, zipfile.ZipFile(target, 'w') as zout:
    for item in zin.infolist():
        info = zipfile.ZipInfo(item.filename, item.date_time)
        info.comment = item.comment
        info.extra = item.extra
        info.compress_type = item.compress_type
        info.create_system = item.create_system
        info.flag_bits = item.flag_bits
        info.internal_attr = item.internal_attr
        info.external_attr = item.external_attr
        if item.filename.startswith('lib/') and item.filename.endswith('.so'):
            info.create_system = 3
            info.external_attr = 0o100700 << 16
        zout.writestr(info, zin.read(item.filename))
PY
zipalign -p -f 4 "$OUT/unsigned-mode.apk" "$OUT/aligned.apk"
zipalign -c 4 "$OUT/aligned.apk" >/dev/null

printf '%s\n' '== 4/5 签名 =='
if [ ! -f "$KS" ]; then
  keytool -genkeypair -v -keystore "$KS" -alias "$KS_ALIAS" \
    -keyalg RSA -keysize 4096 -validity 10950 \
    -storepass "$KS_PASS" -keypass "$KS_PASS" \
    -dname "CN=M365 Gateway v2, OU=Recovery, O=Self-Signed, C=CN"
  echo "已生成新密钥库：$KS —— 请务必备份"
fi
apksigner sign --ks "$KS" --ks-key-alias "$KS_ALIAS" \
  --ks-pass "pass:$KS_PASS" --key-pass "pass:$KS_PASS" \
  --v1-signing-enabled true --v2-signing-enabled true --v3-signing-enabled true \
  --out "$OUT/M365-Gateway-v2.apk" "$OUT/aligned.apk"

printf '%s\n' '== 5/5 验证 =='
apksigner verify "$OUT/M365-Gateway-v2.apk"
aapt dump badging "$OUT/M365-Gateway-v2.apk" | grep -E '^package|application-label|launchable-activity|native-code'
python3 - "$SRC" "$OUT/M365-Gateway-v2.apk" <<'PY'
import hashlib
import sys
import zipfile

old, new = (zipfile.ZipFile(path) for path in sys.argv[1:3])
digest = lambda archive, name: hashlib.sha256(archive.read(name)).hexdigest()
for name in ('lib/arm64-v8a/libm365.so', 'lib/arm64-v8a/libcloudflared.so'):
    info = new.getinfo(name)
    print(name, 'unchanged=', digest(old, name) == digest(new, name),
          'mode=', oct((info.external_attr >> 16) & 0xffff),
          'compress=', info.compress_type)
print('entries:', len(old.namelist()), '->', len(new.namelist()))
PY
(
  cd "$OUT"
  sha256sum M365-Gateway-v2.apk | tee M365-Gateway-v2.apk.sha256
)
echo "完成：$OUT/M365-Gateway-v2.apk"
