#!/usr/bin/env bash
# 用恢复的源码编译 libm365.so，替换进 APK，产出含修复且可正常启动的 v3。
#
# 关键事实：libm365.so 不是 JNI 库，而是被 GatewayService 以
# ProcessBuilder 启动的 PIE 可执行文件（只导出 main.main、
# INTERP=/system/bin/linker64）。恢复的 cmd/server 正是同一形态。
#
# 必须用 -buildmode=pie。-buildmode=exe 会报：
#   runtime.gcdata: missing Go type information for global symbol .dynsym
#
# 这份脚本还固化了两个重打包陷阱：
#   1. manifest package 改名后，组件不能继续使用相对类名，
#      因为 dex 中的类仍位于 com.m365.gateway.*；
#   2. apktool 会把 lib/*.so 的 ZIP 执行权限清成 000，
#      而 Java 用 ProcessBuilder 直接执行 libm365.so，必须恢复 0700。
set -euo pipefail

SRC_INPUT=${1:?用法: build-v3-fixed.sh <原始 base.apk> [输出目录]}
OUT_INPUT=${2:-./out-v3}
REPO=$(cd "$(dirname "$0")/.." && pwd)
SRC=$(realpath "$SRC_INPUT")
OUT=$(realpath -m "$OUT_INPUT")
if [ ! -f "$SRC" ]; then
  echo "原始 APK 不存在: $SRC" >&2
  exit 1
fi
NEW_PKG=com.m365.gateway3
OLD_PKG=com.m365.gateway
NEW_LABEL='M365 网关 v3 修复版'
VERSION_CODE=76
VERSION_NAME=2.24.17
# 密钥库必须固定在仓库内，不能落在输出目录：此前默认 $OUT/...，而每个
# 版本用独立输出目录，keytool 因此每次都新生成一份密钥，导致每版签名都不
# 一样，升级时必须先卸载。签名一致才能覆盖安装并保留数据。
KS=${KS:-$REPO/packaging/keys/m365-gateway-v2.jks}
KS_PASS=${KS_PASS:-[REDACTED]}
KS_ALIAS=${KS_ALIAS:-m365v2}

# go.mod 要求 Go 1.23。优先使用项目固化的 1.23.12，避免系统 Go
# 触发联网 toolchain 自动下载；也允许调用方通过 GO_BIN 覆盖。
if [ -z "${GO_BIN:-}" ]; then
  if [ -x /workspace/toolchain/go1.23/bin/go ]; then
    GO_BIN=/workspace/toolchain/go1.23/bin/go
  else
    GO_BIN=go
  fi
fi
if [[ "$GO_BIN" == */* ]]; then
  GO_BIN=$(realpath "$GO_BIN")
fi
if ! command -v "$GO_BIN" >/dev/null 2>&1 && [ ! -x "$GO_BIN" ]; then
  echo "找不到 Go 编译器: $GO_BIN" >&2
  exit 1
fi

mkdir -p "$OUT"

printf '%s\n' '== 1/6 交叉编译 libm365.so =='
(
  cd "$REPO"
  GOTOOLCHAIN=local GOPROXY=off CGO_ENABLED=0 GOOS=android GOARCH=arm64 GOARM64=v8.0 \
    "$GO_BIN" build -trimpath -buildvcs=false -buildmode=pie -o "$OUT/libm365.so" ./cmd/server
)
readelf -h "$OUT/libm365.so" | grep -E 'Type|Machine|Entry point'
readelf -p .interp "$OUT/libm365.so" | grep -oE '/[a-z/0-9._]+'

printf '%s\n' '== 2/6 反编译 APK =='
rm -rf "$OUT/work"
apktool d -f -o "$OUT/work" "$SRC"

printf '%s\n' '== 3/6 替换 .so、同步 web 资源、改包名与组件 =='
cp "$OUT/libm365.so" "$OUT/work/lib/arm64-v8a/libm365.so"
for f in index.html login.html debug.html; do
  [ -f "$REPO/web/$f" ] && cp "$REPO/web/$f" "$OUT/work/assets/web/$f"
done

python3 - "$OUT/work" "$OLD_PKG" "$NEW_PKG" "$NEW_LABEL" "$VERSION_CODE" "$VERSION_NAME" <<'PY'
import pathlib, re, sys
root = pathlib.Path(sys.argv[1])
old_pkg, new_pkg, label = sys.argv[2:5]
version_code, version_name = sys.argv[5:7]

manifest_path = root / 'AndroidManifest.xml'
s = manifest_path.read_text(encoding='utf-8')
if f'package="{old_pkg}"' not in s:
    raise SystemExit(f'expected package not found: {old_pkg}')
s = s.replace(f'package="{old_pkg}"', f'package="{new_pkg}"', 1)
# Provider authority 是应用私有标识，必须与 provider 内部常量同步。
s = s.replace(f'"{old_pkg}.wake"', f'"{new_pkg}.wake"')
# dex 中的组件类仍是 com.m365.gateway.*。改成完整类名，不能让
# Android 按新 package 把 .MainActivity 解析成不存在的 gateway3.MainActivity。
for component in ('MainActivity', 'AuthActivity', 'DiagActivity', 'TunnelActivity',
                  'WakeProvider', 'GatewayService', 'KeepAliveReceiver', 'BootReceiver'):
    s = s.replace(f'android:name=".{component}"',
                  f'android:name="{old_pkg}.{component}"')
# 应用内广播 action 与新包隔离，避免两个版本互相唤醒。
for action in ('KEEPALIVE', 'START', 'STOP', 'TUNNEL_START'):
    s = s.replace(f'"{old_pkg}.{action}"', f'"{new_pkg}.{action}"')
manifest_path.write_text(s, encoding='utf-8')

strings_path = root / 'res/values/strings.xml'
s = strings_path.read_text(encoding='utf-8')
s = re.sub(r'<string name="app_name">[^<]*</string>',
           f'<string name="app_name">{label}</string>', s, count=1)
strings_path.write_text(s, encoding='utf-8')

yml_path = root / 'apktool.yml'
s = yml_path.read_text(encoding='utf-8')
s = re.sub(r"versionCode: '[^']*'", f"versionCode: '{version_code}'", s, count=1)
s = re.sub(r'versionName: .*', f'versionName: {version_name}', s, count=1)
yml_path.write_text(s, encoding='utf-8')

# 所有 smali 中的应用私有 action、WakeProvider 的 authority/MIME 常量。
smali_dirs = [path for path in root.glob('smali*') if path.is_dir()]
for smali_dir in smali_dirs:
    for path in smali_dir.rglob('*.smali'):
        text = path.read_text(encoding='utf-8')
        old = text
        text = text.replace(f'{old_pkg}.KEEPALIVE', f'{new_pkg}.KEEPALIVE')
        text = text.replace(f'{old_pkg}.START', f'{new_pkg}.START')
        text = text.replace(f'{old_pkg}.STOP', f'{new_pkg}.STOP')
        text = text.replace(f'{old_pkg}.TUNNEL_START', f'{new_pkg}.TUNNEL_START')
        text = text.replace(f'{old_pkg}.wake', f'{new_pkg}.wake')
        if text != old:
            path.write_text(text, encoding='utf-8')

# 静态一致性检查：manifest 里的组件必须在 dex/smali 中存在，且不能
# 因改包名再次留下相对组件名或旧的应用私有标识。
components = ('MainActivity', 'AuthActivity', 'DiagActivity', 'TunnelActivity',
              'WakeProvider', 'GatewayService', 'KeepAliveReceiver', 'BootReceiver')
manifest_after = manifest_path.read_text(encoding='utf-8')
for component in components:
    suffix = old_pkg.replace('.', '/') + '/' + component + '.smali'
    if not any((smali_dir / suffix).exists() for smali_dir in smali_dirs):
        raise SystemExit(f'component class missing: {suffix}')
if re.search(r'android:name="\.(?:' + '|'.join(components) + r')"', manifest_after):
    raise SystemExit('relative application component remains in manifest')
for token in (f'{old_pkg}.wake', f'{old_pkg}.KEEPALIVE', f'{old_pkg}.START',
              f'{old_pkg}.STOP', f'{old_pkg}.TUNNEL_START'):
    for path in [manifest_path, *[p for d in smali_dirs for p in d.rglob('*.smali')]]:
        if token in path.read_text(encoding='utf-8', errors='ignore'):
            raise SystemExit(f'old private identity remains: {token} in {path}')
PY

# 诊断页 cookie 持久化补丁已停用：2.24.15 实测点击「网关诊断」直接闪退。
# 注入位置在构造函数与登录回调内，寄存器/异常表处理不当会导致 Activity
# 初始化即崩溃。保留脚本供后续验证，但不再参与构建。
# python3 "$REPO/packaging/patch-diag-cookie.py" "$OUT/work/smali"

printf '%s\n' '== 4/6 打包（必须使用 aapt2）=='
apktool b "$OUT/work" --use-aapt2 -o "$OUT/unsigned.apk"

# apktool 会丢失 native ZIP entry 的执行权限。恢复为原 APK 的 0700，
# 再交给 zipalign；否则 ProcessBuilder 可能因权限不足启动失败。
python3 - "$SRC" "$OUT/unsigned.apk" "$OUT/unsigned-mode.apk" <<'PY'
import os, sys, zipfile
original, source, target = sys.argv[1:4]
with zipfile.ZipFile(source, 'r') as zin, zipfile.ZipFile(target, 'w') as zout:
    for item in zin.infolist():
        data = zin.read(item.filename)
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
        zout.writestr(info, data)
PY
zipalign -p -f 4 "$OUT/unsigned-mode.apk" "$OUT/aligned.apk"
zipalign -c 4 "$OUT/aligned.apk" >/dev/null

printf '%s\n' '== 5/6 签名 =='
if [ ! -f "$KS" ]; then
  mkdir -p "$(dirname "$KS")"
  keytool -genkeypair -v -keystore "$KS" -alias "$KS_ALIAS" \
    -keyalg RSA -keysize 4096 -validity 10950 \
    -storepass "$KS_PASS" -keypass "$KS_PASS" \
    -dname "CN=M365 Gateway v2, OU=Recovery, O=Self-Signed, C=CN"
  echo "已生成密钥库 $KS —— 请备份"
fi
printf '签名密钥指纹: '
keytool -list -keystore "$KS" -storepass "$KS_PASS" 2>/dev/null | grep -oE '\(SHA-256\): [0-9A-F:]+' || true
apksigner sign --ks "$KS" --ks-key-alias "$KS_ALIAS" \
  --ks-pass "pass:$KS_PASS" --key-pass "pass:$KS_PASS" \
  --v1-signing-enabled true --v2-signing-enabled true --v3-signing-enabled true \
  --out "$OUT/M365-Gateway-v3-fixed.apk" "$OUT/aligned.apk"

printf '%s\n' '== 6/6 验证 =='
apksigner verify "$OUT/M365-Gateway-v3-fixed.apk"
aapt dump badging "$OUT/M365-Gateway-v3-fixed.apk" | grep -E '^package|application-label|launchable-activity|native-code'
python3 - "$SRC" "$OUT/M365-Gateway-v3-fixed.apk" <<'PY'
import hashlib, sys, zipfile
old, new = (zipfile.ZipFile(p) for p in sys.argv[1:3])
h = lambda z, n: hashlib.sha256(z.read(n)).hexdigest()
so = 'lib/arm64-v8a/libm365.so'
cf = 'lib/arm64-v8a/libcloudflared.so'
print('libm365.so 已替换 :', h(old, so) != h(new, so))
print('libcloudflared 未动:', h(old, cf) == h(new, cf))
for n in (so, cf):
    i = new.getinfo(n)
    print(n, 'mode=', oct((i.external_attr >> 16) & 0xffff), 'compress=', i.compress_type)
print('条目数            :', len(old.namelist()), '->', len(new.namelist()))
PY
(
  cd "$OUT"
  sha256sum M365-Gateway-v3-fixed.apk | tee M365-Gateway-v3-fixed.apk.sha256
)
echo "完成：$OUT/M365-Gateway-v3-fixed.apk"
