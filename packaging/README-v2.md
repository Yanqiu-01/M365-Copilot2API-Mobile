# M365 网关 v2（可与原版并存）

这是保留原始业务二进制、仅改包名并重签名的 ARM64 Android 版本；可与原版 `com.m365.gateway` 并存。它不包含恢复源码中的思考时长、评测记账或 DNS/PKCE 代码修复；需要这些修复请使用 v3。

## 当前交付版本

| 项目 | 值 |
|---|---|
| APK | `M365-Gateway-v2.apk` |
| 包名 | `com.m365.gateway2` |
| 应用名 | `M365 网关 v2` |
| versionName / versionCode | `2.24.1` / `60` |
| ABI | `arm64-v8a` |
| SHA-256 | `53ca9b6c22d4d0e719531ec427035835426aa8f6fe2f6c670bb06862e4a82f80` |

`2.24.1 / 60` 是对此前 `2.24.0 / 59` v2 的覆盖更新。两个版本包名和签名相同，可以直接升级；原版和 v3 不受影响。

## 本次启动修复

此前 v2 的 manifest 仍使用 `.MainActivity` 等相对组件名。改包名后 Android 会把它解析为 `com.m365.gateway2.MainActivity`，但 DEX 中的类实际仍位于 `com.m365.gateway.*`，因此可能在点击图标时闪退。

当前版已同步修正：

1. Activity、Service、Receiver、Provider 全部使用 DEX 中真实存在的完整类名 `com.m365.gateway.*`；
2. `WakeProvider` 的 authority 和 MIME 常量改为 `com.m365.gateway2.wake`；
3. 应用内广播 action 使用 `com.m365.gateway2.*`，避免和原版串扰；
4. 恢复 `lib/arm64-v8a/*.so` 的 ZIP 执行权限（`0700`）。`GatewayService` 通过 `ProcessBuilder` 直接启动 `libm365.so`，apktool 重打包后产生的 `000` 权限不可用。

## 与原版的内容关系

以下内容均与原 APK 逐字节一致：

- `lib/arm64-v8a/libm365.so`；
- `lib/arm64-v8a/libcloudflared.so`；
- `assets/web/index.html`、`login.html`、`debug.html`；
- `assets/ca-certificates.crt`。

变化仅包括 Android 身份/资源重编译、应用内私有标识同步、版本号和签名；APK 条目数保持 `22 -> 22`。

## 已完成的验证

- 包名：`com.m365.gateway2`；
- 启动 Activity：`com.m365.gateway.MainActivity`；
- 8 个 manifest 应用组件均能在 DEX/smali 中找到；
- 无相对组件名残留；
- Provider authority/MIME 与 manifest 一致；
- 两个 native entry 未压缩且带可执行权限；
- `zipalign` 通过；
- APK Signature Scheme v2、v3 通过；
- 从最终 APK 提取出的 ARM64 `libm365.so` 可在 QEMU AArch64 环境启动 HTTP 服务；`/`、`/login`、管理员登录和鉴权后的 `/api/health` 均通过（可用 `packaging/qemu-smoke.sh` 重跑，不等同于真实 Android framework 测试）。

工作区没有 `adb`、Android emulator 或真机，因此尚未完成真实设备上的点击启动测试；请在 ARM64 Android 设备上实测。

## 安装与升级

```bash
adb install -r M365-Gateway-v2.apk
```

也可以在 Android 文件管理器中打开 APK，并允许安装未知来源应用。若已有旧 v2（`com.m365.gateway2`），应选择升级安装；如提示签名冲突，只卸载旧的 `com.m365.gateway2` 后再装（会清除该包数据），不要卸载原版或 v3。

首次使用需重新配置账号与 API Key；Android 按包名隔离应用数据。

APK 仅支持 `arm64-v8a`。自签名证书出现“未知来源”提示属于预期行为。

## 密钥库

v2 与 v3 共用：`m365-gateway-v2.jks`

- 别名：`m365v2`
- 库口令 / 密钥口令：由 `KS_PASS` 环境变量提供（不入库）
- 证书 SHA-256：`8199a4c91043d857ffc88303ab2949639c1337455fd0ffd0891d50d88d27b418`

请备份密钥库。后续 v2/v3 更新必须使用同一密钥，否则无法覆盖安装。
