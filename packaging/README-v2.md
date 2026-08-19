# M365 网关 v2（可与原版并存）

## 这是什么

把原 APK 改包名后重新签名的版本，可与你当前安装的原版**并存**，
互不覆盖。

| 项 | 原版 | v2 |
|---|---|---|
| 包名 | `com.m365.gateway` | `com.m365.gateway2` |
| 应用名 | M365 Copilot 网关 | M365 网关 v2 |
| 版本 | 2.24.0 (59) | 2.24.0 (59) |
| 签名 | 原作者密钥 | 本仓库自签名 |

## 重要：这个 APK 不含本轮的代码修复

重打包用的是**原 APK 里的 `libm365.so`**（已校验逐字节一致），
不是恢复出的 Go 源码编译产物。

所以以下修复**不在这个 APK 里**：
- 思考时长显示为 0 的修复（f618502）
- 评测重复记账的修复（379d4a4）

原因：APK 的 `.so` 带 JNI 入口，而恢复的源码是 `cmd/server` 可执行文件，
两者入口不同。要产出含修复的版本，需要先补 JNI 封装层再交叉编译替换 `.so`。

这个 v2 的作用是**让你能并存安装、不必反复覆盖**，功能与原版完全相同。

## 安装

```
adb install M365-Gateway-v2.apk
```

首次使用需重新配置账号与 API Key —— Android 按包名隔离存储，
新应用读不到原版的数据。

## 密钥库：请务必备份

`m365-gateway-v2.jks`
- 别名 `m365v2`
- 库口令 / 密钥口令：`[REDACTED]`
- 证书 SHA-256：`8199a4c91043d857ffc88303ab2949639c1337455fd0ffd0891d50d88d27b418`

以后所有 v2 更新都必须用这个密钥签名，否则无法覆盖安装。
**弄丢就只能再换一次包名**。

## 改动清单

只改了三处，未触碰任何业务逻辑：

1. `AndroidManifest.xml`
   - `package`: `com.m365.gateway` → `com.m365.gateway2`
   - `WakeProvider` 的 `android:authorities` 同步改名
     （必须改，否则两版并存会因 authority 冲突而安装失败）
2. `res/values/strings.xml` 的 `app_name`
3. 四个应用内广播 action 加 `2` 后缀（smali 6 个文件共 15 处 + manifest）
   - `KEEPALIVE` / `START` / `STOP` / `TUNNEL_START`
   - 目的是避免两个版本互相唤醒或串扰

**smali 类路径保持原样**（仍是 `com/m365/gateway/...`）。
Android 只按 manifest 的 `package` 识别应用身份，改类路径没必要且易出错。

## 校验

```
签名          v2 + v3 通过（v1 未启用，minSdk 26 无需 v1）
libm365.so    与原包逐字节一致
libcloudflared.so  一致
assets/web/*  一致
条目数        22 → 22，无缺失
zipalign      4 字节对齐通过
```

`classes.dex` 因修改 action 字符串而重新编译，属预期变化。
