# 源码恢复说明

## 恢复来源

本项目源码从 `base.apk` 中恢复，结合：
1. APK 中保留的完整资源文件（HTML/CSS/JS）
2. 上游开源仓库 HEXUXIU/M365-Copilot2API 的基础代码
3. 从二进制 `libm365.so` 中推断的独创实现

## 文件状态

> 这是从 APK 反编译、符号/行号证据和上游源码交叉恢复的工程，**不是声称与原始源码逐字节一致的完整源码备份**。恢复代码已通过本地 Go 测试、vet、Linux 构建和 ARM64 Android PIE 构建；真实 Android framework/真机启动仍需设备验收。

### ✅ 已验证可用
- `assets/web/*.html` - 前端界面
- `go.mod`, `go.sum` - 依赖配置
- `cmd/server` - 入口程序
- `internal/web/usage.go` - 已应用修复补丁
- `internal/auth/*` - 认证模块
- `internal/outbound/*` - 代理池
- 大部分 `internal/web/*.go` - 核心Web处理

### ⚠️ 仍需持续审计/补证据的部分
- `internal/chathub/reasoning_recover.go` - 从二进制推断的框架，核心逻辑已实现
- `internal/chathub/client_timing.go` - 新增的思考时间修复
- `internal/web/reasoning_delta.go` - 从二进制推断的实现
- `internal/chathub/wire_capture.go` - 框架已有，需补充存储逻辑
- `internal/chathub/client_identity.go` - 仅框架
- `internal/web/account_cooldown.go` - 基础实现已有
- `internal/web/router_frames_diag.go` - 基础框架
- `internal/mcp/http.go` - 仅占位符

### ❌ 当前边界
没有声称“无缺口”：全量审计报告列出了仍需逐函数核对的差异，见 `audit/FULL-AUDIT-2026-08-19.md` 和 `audit/PROGRESS.md`。

## 关键修复

1. **思考时间计算** - `internal/chathub/client_timing.go`
   - 正确记录 `thinkingStarted` 和 `thinkingEnded`
   - 先发空白拉起客户端计时器
   
2. **用量记录** - `internal/web/usage.go`
   - 添加 `ThinkingMs` 字段
   - 汇总 `avg_thinking_ms`

3. **思考内容恢复** - `internal/chathub/reasoning_recover.go`
   - 从帧中提取 `hiddenText`
   - 缓存 ChainOfThought

## 编译

```bash
go mod tidy
go build -o m365-copilot2api ./cmd/server
```

## 编译 Android ARM64

```bash
GOTOOLCHAIN=local GOPROXY=off CGO_ENABLED=0 \
GOOS=android GOARCH=arm64 GOARM64=v8.0 \
/workspace/toolchain/go1.23/bin/go build -trimpath -buildvcs=false \
-buildmode=pie -o libm365.so ./cmd/server
```

## 测试

```bash
go test ./internal/...
```

## 后续审计边界

不要把测试通过误读为 APK 完整等价。后续如继续恢复，应以 `audit/FULL-AUDIT-2026-08-19.md`、`audit/apk-missing-funcs-2026-08-19.txt` 和 `audit/PROGRESS.md` 的证据清单为准，逐项补齐或定性；不应凭空添加 APK 中不存在的页面、路由或函数。
