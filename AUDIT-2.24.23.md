# 只读审计报告 · 2.24.23 (82)

> **后续处理**：A/B/C 三项已在 2.24.24 (83) 修复，详见文末「修复记录」。

审计对象：提交 `7b1570b`，APK SHA-256 `2ce6b2fa…be3fbd`
性质：只读，未修改任何实现代码。

规模：非测试 100 文件 / 19562 行，测试 94 文件 / 8882 行（测试占比 45%）。

## 闸门状态

| 项 | 结果 |
|---|---|
| `go build ./...` | 通过 |
| `go vet ./...` | 无告警 |
| `go test ./...` | 全部通过 |
| Android arm64 PIE | 通过 |
| `go test -race` | **无法执行**（proot 沙箱 VMA 限制，非代码问题） |
| `gofmt -l` | 14 文件有差异（纯空行/对齐，无语义影响） |
| 硬编码凭据扫描 | 未发现 |
| 依赖 | 4 直接 + 1 间接，全部固定版本 |

## 需要处理的发现

### A. 帧脱敏存在实质缺口（安全，最高优先）

`sanitizeWirePayload` → `redactWireValue` 只按 **JSON key 名**删除凭据字段。
实测 5 种形态，4 种未被脱敏：

| 形态 | 结果 |
|---|---|
| `{"a":{"b":{"access_token":"…"}}}` | 已脱敏 |
| `{"headers":{"Cookie":"m365_admin=abcdef123456"}}` | **泄漏** |
| `{"headers":{"Set-Cookie":"session=deadbeef"}}` | **泄漏** |
| `{"text":"curl -H 'Authorization: Bearer eyJabc.DEF.ghi'"}` | **泄漏** |
| `{"note":"token is eyJhbGciOiJIUzI1NiJ9.payload.sig"}` | **泄漏** |

两个原因：`cookie` / `set-cookie` 不在 key 清单里；凭据出现在**字符串值内部**时
按 key 删除完全无效。

放大因素：本轮把 `/api/admin/debug/wire` 加入了本机免密豁免，理由是"帧内容已脱敏"
——该前提实测不成立。影响面限于本机（非回环仍需鉴权），但前提本身是错的。

建议：key 清单补 `cookie`/`set-cookie`/`x-api-key`/`proxy-authorization`；
对字符串值增加模式擦除（`Bearer\s+\S+`、`eyJ[A-Za-z0-9_-]{10,}\.\S+`、
`\w+=[A-Za-z0-9]{16,}`）。修完后应重新评估是否保留 wire 端点的免密豁免。

### B. 后台 goroutine 无 panic 保护（稳定性）

9 个 goroutine 启动点，只有 2 处 recover，且 `recoverPanics` 是 HTTP 中间件，
只覆盖请求 goroutine。裸奔的 7 处中两处在评测路径：

- `benchmark.go:581` — `go func() { s.openaiChat(recorder, request); … }()`
- `benchmark.go:1071` — 评测任务串行推进的主循环

这两处在中间件作用域**之外**。`openaiChat` 一旦 panic，会直接终止 Go 子进程；
用户侧表现是服务重启、管理会话丢失（`adminSessions` 是纯内存 map）——与此前
排查过的"登录态莫名丢失"症状一致，属于同一类现象的另一条成因。

建议：给这两处加 `defer recover` 并落日志，把失败收敛为单个任务 error。

### C. 工具结果预算与写入无上限叠加（本轮引入）

本轮把观察类工具结果放开到 64 KiB 以修复"模型看不到完整源码"。同时：

- `write_file` 不校验 content 长度；
- `messages` 只追加、从不裁剪。

理论上界约 14 步 × 64 KiB ≈ 900 KiB 上下文。不会 OOM，但可能触发上游 token
上限而使任务以上游错误告终。当前评测夹具最大文件 1724 字节，实际不会触发。

建议：`write_file` 加单文件上限（如 256 KiB）并在超限时返回明确错误。

## 已确认无回归

- 伪造预热器 `preheater.go`：已清除，仅存防回归测试
- `isUnavailableModelReply`：已删除
- `classifyUpstream` / `upstreamStageError` / `toolLooksObservational` /
  `normalizeDecisionText` / `stripDecorations`：均存在
- `session_resolver.go`：UTF-8 合法、无 BOM（此前记录的乱码已修）
- `patch-diag-cookie.py`：构建脚本中已注释停用（148 行），不参与打包
- 本轮豁免清单保持最小：登录、改密、建密钥、设置等永不豁免，有测试固化

## 遗留（低优先，非回归）

- `chathub.writeBudget`：原 APK 有，恢复版仍缺失。未观察到由此引发的症状。
- `gradeReport.check`：APK 无此方法，恢复版 26 处调用。属恢复期自建辅助，
  行为正确，仅与原版实现形状不一致。
- `web/debug.html`：6567 字节死文件，`/debug` 路由在上游与原版 APK 均不存在。
- `gofmt` 14 文件差异：建议某次提交顺带 `gofmt -w`。

## 优先级

1. A（安全，且推翻了本轮一处豁免的前提）
2. B（可解释已观察到的会话丢失现象）
3. C（本轮引入，当前不触发）
4. 遗留项按需处理

## 修复记录 · 2.24.24 (83)

### A 帧脱敏（已修）
`credentialKeys` 补齐 cookie / set-cookie / x-api-key / proxy-authorization /
id_token / sessionid 等；新增 `valueCredentialPatterns` 对字符串**值内部**做模式
擦除（`Bearer|Basic|Negotiate` 后的令牌、`eyJ…` JWT、`name=value` 长串），
数组元素同样处理。`wire_redaction_test.go` 覆盖 8 种形态，并断言诊断信息
（target / model / 正文）不被误删、短赋值（`retry=3`、`id=42`）不被误擦。

### B goroutine panic 保护（已修）
新增 `web.safeGo` / `safeGoWithCleanup` 与 `chathub.safeGoDeliver`。
chathub 两处是 channel 交付模式，只加 recover 会把崩溃换成永久阻塞，
因此 `safeGoDeliver` 强制在 panic 分支交付兜底错误。已接入 5 处：
benchChat 自调用、评测主循环（panic 时把运行状态收敛为 error 而非卡在
running）、瞬态会话删除、auto_cleanup 长驻循环、uploadAttachments、
conn.ReadMessage。

未改动的 3 处经复核确认安全：`protocol_handlers.go:50` 自带 recover；
`proxy.go:259/270` 仅调用 `dialer.Dial`（返回 error 不 panic）与判 nil 后
`Close`，channel 有缓冲。

### C write_file 上限（已修）
`benchMaxFileBytes = 256 KiB`，超限返回明确错误。正常写入不受影响。

### 顺带
`gofmt -w` 全仓统一，剩余差异 0。

### 验证
build / vet / 全部测试 / Android arm64 PIE 均通过；QEMU 冒烟确认免密豁免
与鉴权边界不变（4 个只读端点与 2 个开关 200，settings/keys 仍 401）。
### race 检测：已查明根因，改用压力测试替代

`go test -race` 在本环境无法运行，原因已查实（不是配置问题）：

Android 内核用 39 位用户地址空间（`CONFIG_ARM64_VA_BITS=39`），而 Go 的 arm64
TSan 运行时硬编码要求 48 位。验证过程：
1. race 二进制**能正常编译**（27 MB），说明只是运行期拦截；
2. `TSAN_OPTIONS` 无任何相关开关；
3. QEMU 用户态继承宿主内核布局，实测 mmap 仍返回 39 位地址；
4. 反汇编 `race_linux_arm64.syso`，定位 `__tsan::InitializePlatformEarly` 中的
   `clz` → `cmp w0, #0x30` → `b.ne` → `Printf` + `Die()`；
5. 把 `b.ne` 改成 `nop` 绕过检查后，报错变成
   `failed to allocate 0x2010000 bytes at address 200002b60000` ——
   TSan 要在约 35 TB 处映射影子内存，而 39 位空间上限 512 GB，该地址不存在。

结论：这是内核编译选项，非 root 不可更改，无法绕过。syso 已还原（SHA-256 校验
一致）。

替代方案：新增 `concurrency_stress_test.go`，6 项高并发压力测试 +
状态一致性断言：

| 测试 | 覆盖 | 规模 |
|---|---|---|
| AdminSessionAccess | adminSessions 并发读写与过期清理 | 40 goroutine × 150 次 |
| SettingsAccess | 设置高频读 + 并发写 | 30 读 + 6 写 |
| UsageRecording | 用量累加不丢记录 | 32 × 100，断言总数 |
| FrameCapture | 抓帧并发写入 | 24 × 80 |
| AccountStateUpdates | 冷却/健康状态并发更新 | 24 goroutine |
| HTTPRequests | 端到端：中间件+鉴权+设置读取 | 1280 次请求 |

关键：前两版这些测试有两项是**空转**（帧捕获开关默认关闭、`MarkFailure` 需要
可识别的错误类型），实测"保留 0 组帧""健康记录 0 条"。已加
`t.Fatal` 断言强制要求真正触达代码路径，现在分别记录 50 组帧与 3 条健康记录。

以 `GOMAXPROCS=8` 重复 30 轮全部通过，Go 的 map 并发读写会直接 fatal
（不可 recover），因此这类问题若存在会稳定暴露。

局限：压力测试不保证捕获所有数据竞争，尤其是低频窗口。若将来能在 48 位环境
（x86_64 机器、或 `CONFIG_ARM64_VA_BITS=48` 的内核）运行，仍应补跑
`go test -race ./...`。
