# 源码恢复说明

## 恢复来源

本项目源码从 `base.apk` 中恢复，结合：
1. APK 中保留的完整资源文件（HTML/CSS/JS）
2. 上游开源仓库 HEXUXIU/M365-Copilot2API 的基础代码
3. 从二进制 `libm365.so` 中推断的独创实现

## 文件状态

### ✅ 完全恢复（100%可用）
- `assets/web/*.html` - 前端界面
- `go.mod`, `go.sum` - 依赖配置
- `cmd/server` - 入口程序
- `internal/web/usage.go` - 已应用修复补丁
- `internal/auth/*` - 认证模块
- `internal/outbound/*` - 代理池
- 大部分 `internal/web/*.go` - 核心Web处理

### ⚠️ 部分恢复（需要补充实现）
- `internal/chathub/reasoning_recover.go` - 从二进制推断的框架，核心逻辑已实现
- `internal/chathub/client_timing.go` - 新增的思考时间修复
- `internal/web/reasoning_delta.go` - 从二进制推断的实现
- `internal/chathub/wire_capture.go` - 框架已有，需补充存储逻辑
- `internal/chathub/client_identity.go` - 仅框架
- `internal/web/account_cooldown.go` - 基础实现已有
- `internal/web/router_frames_diag.go` - 基础框架
- `internal/mcp/http.go` - 仅占位符

### ❌ 无法恢复（编译后丢失）
无 - 所有必需文件已提供框架

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
export CGO_ENABLED=0
export GOOS=android
export GOARCH=arm64
go build -o libm365.so -buildmode=c-shared ./cmd/server
```

## 测试

```bash
go test ./internal/...
```

## 已知需要补充的部分

查看标记为 `// TODO:` 的注释，这些是需要你补充实现细节的地方。

主要集中在：
1. `wire_capture.go` 的帧存储逻辑
2. `client_identity.go` 的身份管理细节
3. `mcp/http.go` 的 MCP 协议实现

其他核心功能已完整。
