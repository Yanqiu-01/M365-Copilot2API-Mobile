# M365 网关 v3 修复版

这是与原版、v2 可并存安装的 ARM64 Android 改名版，并包含恢复源码中的实际代码修复。

## 当前交付版本

| 项目 | 值 |
|---|---|
| APK | `M365-Gateway-v3-fixed.apk` |
| 包名 | `com.m365.gateway3` |
| 应用名 | `M365 网关 v3 修复版` |
| versionName / versionCode | `2.24.4` / `63` |
| ABI | `arm64-v8a` |
| SHA-256 | `078fe9e55b5e033f0b4cc60b06196629598ecb882b100d049523abd5a2a071f5` |

本次 `2.24.4 / 63` 是对此前 `2.24.3 / 62` v3 的覆盖更新；两者包名和签名相同，可以直接升级。原版 `com.m365.gateway` 与 v2 `com.m365.gateway2` 不受影响。

## 本次 2.24.4 修复：模型目录与路由回归原版

以在 QEMU 中运行原始 `libm365.so` 的实测结果为准，修正了此前恢复源码擅自改动的模型面板：

1. **模型目录恢复为原版内容**
   `/api/admin/models` 与 `/v1/models` 现在返回与原 APK 完全一致的目录：11 个内置公开模型加上设置中的 3 个可配置别名，默认共 14 项。此前恢复版只暴露 3 个 `gpt-5.6-*` 别名，把内置模型全部藏掉了，前端“模型测试”页因此几乎无模型可选。
2. **补回 `gpt-5.3-reasoning` 路由**，并移除原 APK 中不存在的 `claude-opus-4-*`、`claude-sonnet-4-6`、`claude-fable-5`、`gpt-image-2` 等条目和对应 tone。
3. **移除虚构的动态同步链路**
   原 APK 没有 `/api/admin/models/sync`，也不会去 CDN 抓取 tone 列表。相关端点、后台抓取和缓存已删除，上游 tone 恢复为固定 11 项。
4. **默认参数回归原版**
   上下文窗口 `262144`、聊天超时 `600` 秒、图片超时 `180` 秒、`clientProfile` 默认 `office`、三个可配置别名默认档位均为 `xhigh`。
5. **评测默认模型改回稳定内置路由**
   空模型评测使用 `gpt-5.6-reasoning`，不再默认落到依赖租户开通情况的 `gpt-5.6-sol` 别名；评测端点响应结构也回归原版。
6. **前端不再过滤内置模型**
   模型测试和评测下拉恢复展示完整目录。

`Gpt_5_2_Chat`、`Gpt_5_6_Reasoning` 这类名称是服务端内部的 ChatHub tone，只用于把公开模型路由到上游，不作为模型 ID 暴露；公开模型名中不存在任何 `-chat` 后缀。

## v3 改名启动修复

此前改包名后点击 v3 立即闪退，原因是 manifest 中的组件使用相对类名（例如 `.MainActivity`）。Android 会将其解析为 `com.m365.gateway3.MainActivity`，但 DEX 中实际类仍位于 `com.m365.gateway.*`。

最终版已修正：

1. manifest 中的 Activity、Service、Receiver、Provider 全部改为 DEX 中真实存在的完整类名 `com.m365.gateway.*`；
2. `WakeProvider` 的 authority 和 MIME 常量同步为 `com.m365.gateway3.wake`；
3. 应用内部广播 action 使用 `com.m365.gateway3.*`，避免与原版及 v2 串扰；
4. 恢复 `lib/arm64-v8a/*.so` 的 ZIP 执行权限（`0700`）。`GatewayService` 使用 `ProcessBuilder` 直接启动 `libm365.so`，权限不能是 apktool 重打包后产生的 `000`。

## 本次 2.24.3 修复

1. **思考档位对象显示异常**
   前端现在兼容 `{effort, description}` 形式的 Codex 推理档位，并正确发送 `max`；评测默认优先选择设置中显式配置的模型路由。

2. **模型拒绝不再误报成功**
   管理页模型探测和非流式聊天会识别上游“抱歉，我无法响应”等短拒绝，返回 `model_unavailable`；模型批量测试改为串行，避免并发探测触发误判的限流。

3. **新增回归测试**
   覆盖 `max` 档位、默认路由选择、拒绝响应识别和纯函数路径，避免测试污染全局设置。

## 包含的代码修复

1. **思考时长显示为 0**（提交 `f618502`）
   思考内容此前只在完成帧被一次性补发，客户端看到首末增量几乎同时抵达，时长算成 0。现改为逐帧实时推送，取材范围与兜底路径统一，并补充 `candidateMessages` 下钻。

2. **评测重复计入用量**（提交 `379d4a4`）
   评测自调用经过 `openaiChat` 时不再触发重复的普通聊天记账；内部调用由 `X-M365-Internal-Call` 标记。

3. **实现 `M365_DNS` 与 `M365_PROMPT`**（提交 `7bd0974`）
   Android 注入的自定义 DNS、登录 prompt 和根证书路径探测现在由恢复的 Go 代码消费。

4. **清除对话详情页虚构链**（提交 `633cea6`）
   删除 APK 中不存在的 `/conversation` 页面和 `/api/m365/conversations/detail` 恢复链路，保留已由真实资源和代码支持的功能。

## 二进制信息

`libm365.so` 不是 JNI 库，而是由 `GatewayService` 通过 `ProcessBuilder` 启动的 ARM64 PIE 可执行文件：

```text
ELF        64-bit AArch64
Type       DYN (Position-Independent Executable file)
INTERP     /system/bin/linker64
Entry      0x86730
导出       main.main
CGO        disabled
```

它由项目固化的 Go `1.23.12` 交叉编译：

```text
GOOS=android GOARCH=arm64 GOARM64=v8.0
-buildmode=pie
```

`libcloudflared.so` 未修改，APK 条目数保持 `22 -> 22`。

## 已完成的验证

- `go test ./...`：全部通过；
- `go vet ./...`：通过；
- ARM64 Android PIE 交叉编译：通过；
- `zipalign`：通过；
- `apksigner`：APK Signature Scheme v2、v3 通过；
- `aapt dump badging`：包名、版本、启动 Activity、ABI 与预期一致；
- manifest 8 个应用组件均能在 DEX/smali 中找到；
- 两个 native entry 均为未压缩、可执行权限；
- `libcloudflared.so` 与原 APK 内容一致；
- 从最终 APK 提取出的 `libm365.so` 在 QEMU AArch64 环境中启动成功：`/`、`/login`、管理员登录和鉴权后的 `/api/health` 均返回 200；可用 `packaging/qemu-smoke.sh` 重跑。
- 端点级原版对照：最终 APK 的 `/api/admin/models` 与原始 APK 的实测响应逐字段一致（14 项模型、全部元数据），`/api/admin/settings` 除监听端口外完全一致，`upstreamTones` 与 `codexModels` 一致，`/api/admin/models/sync` 与原版同为 404。这些断言已内置到 `packaging/qemu-smoke.sh` 和 `internal/web/apk_contract_test.go`（夹具取自原始二进制实测结果）。

QEMU 只验证了 Go 子进程和 HTTP 服务，不提供 Android framework、Manifest 组件解析或真实 nativeLibraryDir 环境。工作区没有 `adb`、Android emulator 或真机，因此**尚未完成真实设备上的点击启动测试**。静态检查已经覆盖本次已定位的闪退原因，但仍请在你的 ARM64 Android 设备上实测。

## 安装与升级

```bash
adb install -r M365-Gateway-v3-fixed.apk
```

也可以直接在 Android 文件管理器中打开 APK，并允许安装未知来源应用。若设备上已有此前的 v3（`com.m365.gateway3`），应选择升级安装；如果提示签名冲突，只卸载旧的 `com.m365.gateway3` 后再装（会清除该包的数据），不要卸载原版或 v2。

首次使用需要重新配置账号和 API Key；Android 按包名隔离应用数据。

APK 仅支持 `arm64-v8a`。自签名证书出现“未知来源”提示属于预期行为。

## 密钥库

v2 与 v3 共用：`m365-gateway-v2.jks`

- 别名：`m365v2`
- 口令：`[REDACTED]`
- 证书 SHA-256：`8199a4c91043d857ffc88303ab2949639c1337455fd0ffd0891d50d88d27b418`

请备份密钥库。后续更新必须使用同一密钥，否则无法覆盖安装。
