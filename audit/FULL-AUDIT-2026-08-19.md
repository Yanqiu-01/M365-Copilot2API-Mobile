# 仓库全量审计报告

日期：2026-08-19
基线 APK：`delivery/original/base.apk`
APK sha256：`374ab2e2e0a456cebcdbe5f384b3f2774e84b5950985659f3f929fc2028b5029`
审计对象：`/workspace/apk-recovery` @ `13d35c4`

## 方法

APK 自身构建信息（`go version -m` 读出，非推断）：

```
go1.23.12
path  m365-copilot2api/cmd/server
build -buildmode=exe -compiler=gc -trimpath=true
      CGO_ENABLED=0 GOOS=android GOARCH=arm64 GOARM64=v8.0
```

本地工具链已对齐为 go1.23.12，因此行号级比对可信。

`libm365.so` 的 `.text` 已被 strip 符号表，`go tool objdump` 不可用，
无法做字节级常量提取。可用证据限于：

1. `.data.rel.ro.gopclntab` 的函数表（顶层符号 + 每函数精确行段）
2. inline tree（`PCToLine` 第三返回值可穿透内联，暴露被内联函数真名）
3. 二进制字符串表（`strings -a`）

### 三级收敛

单纯比较符号名会产生大量假阳性，因为小函数被内联、无调用点被
链接器裁剪后都不出现在顶层符号表。故采用三级收敛：

| 阶段 | 判据 | 结果 |
|---|---|---|
| L0 | APK 顶层符号 447 vs 仓库 639，直接差集 | 244 |
| L1 | 穿透 inline tree，APK 可见符号升至 783 | 244（无变化） |
| L2 | 对照实验：本地同参数编译产物可见符号 821，取交集 | **90** |
| L3 | 剔除"整个文件不在 APK"的 A 类 | **50** |

L2 是关键：若某函数在**本地产物中也不可见**，说明它被内联或
死代码消除，那么它在 APK 中不可见属正常现象，不能判为虚构。
该步骤排除了 154 个假阳性。

## 结论一：文件级差距

### APK 有、仓库无（1 个）

```
cmd/server/net_mobile.go
```

### 仓库有、APK 无（11 个，A 类）

```
internal/auth/device.go
internal/chathub/preheater.go
internal/config/config.go
internal/mcp/client.go
internal/mcp/queue.go
internal/mcp/stdio.go
internal/mcp/tools.go
internal/web/account_concurrency.go
internal/web/account_health.go
internal/web/public_identity.go
internal/web/xml_tools.go
```

这 11 个文件整体不在 APK 的 81 个源文件中。它们不是"未被调用
所以裁掉"——pclntab 记录的是编译期信息，文件若参与编译至少
会留下痕迹。需要单独定性：是上游遗留、是二次开发新增，还是
恢复过程中虚构。

注意 `internal/web/public_identity.go` 一个文件就贡献了 27 个
可疑符号，是最大单点。

## 结论二：已确证的虚构实现

### 1. `internal/web/stream_text.go`（本轮确证）

APK 侧行段：

```
 15-20    streamTextWithToolLookahead
 23-90    flushStreamText
 93-119   declaredFenceStart
```

三个函数占满 15-119，文件到此结束。

本地侧行段：

```
 15-20    streamTextWithToolLookahead
 26-69    flushStreamText          <- 压缩了 24 行
 75-101   declaredFenceStart
107-121   declaredFencePrefix      <- APK 不存在
124-136   completeFenceEnd         <- APK 不存在
141-152   keepRuneTail             <- APK 不存在
```

对 APK 的 `flushStreamText` 做 inline tree 分析，其 `via` 字段
全部指向自身，仅内联了 `strings.Builder` 与 `strings` 标准库，
**未内联任何项目内函数**。即 APK 中该函数是 68 行单体实现，
围栏收尾与 UTF-8 尾部保留逻辑直接写在体内。

判定：`declaredFencePrefix`、`completeFenceEnd`、`keepRuneTail`
是前人重构时从 `flushStreamText` 拆出的产物，APK 中不存在。

### 2. `internal/web/session_resolver.go`（上一轮已确证并修复）

`matchSuffixLocked` 与 `suffixMatchLen` 在 APK 447 个符号中零命中。
字符串表存在 `context_prefix_` 与 `context_similar_`，
**不存在** `context_suffix_`。真实实现是相似度兜底。

已于 `13d35c4` 修正：删除后缀匹配，恢复
`contextSimilarity` / `jaccardSimilarity` / `tokenize` / `matchSimilarLocked`。

### 3. 前端测试断言（本轮发现）

`internal/web` 全量测试有 2 项 FAIL：

- `TestConversationDetailPageContainsCompleteViews` 读 `web/conversation.html`
- `TestWebIndexDefaultsToChineseUntilLocaleIsSelected` 读 `web/index.html`
  并断言含 `const localeSelectionKey='m365_locale_selected';`

APK 的 `assets/web/` 实际只有三个文件：

```
debug.html    6567
index.html  102579
login.html   10611
```

其中 `index.html` **不含** `localeSelectionKey` 标记，
`conversation.html` **根本不存在**。

判定：这两个测试断言的是 APK 中不存在的前端特性，与
`matchSuffixLocked` 同属虚构。不应通过伪造 HTML 文件让测试转绿。

## 结论三：行段规模反向信号

对 20 个含 B 类嫌疑的文件，比较 APK 与本地的最大行号：

| 文件 | APK | 本地 | 差值 | 方向 |
|---|---|---|---|---|
| internal/web/toolloop.go | 58 | 244 | +186 | 本地膨胀 |
| internal/web/images.go | 172 | 535 | +363 | 本地膨胀 |
| internal/web/conversations.go | 228 | 341 | +113 | 本地膨胀 |
| internal/web/codex_catalog.go | 248 | 324 | +76 | 本地膨胀 |
| internal/auth/token.go | 145 | 214 | +69 | 本地膨胀 |
| internal/web/cache_stats.go | 129 | 163 | +34 | 本地膨胀 |
| internal/web/stream_text.go | 119 | 152 | +33 | 本地膨胀（已确证） |
| internal/web/session_resolver.go | 569 | 599 | +30 | 本地膨胀 |
| internal/web/router_frames_diag.go | 127 | 160 | +33 | 本地膨胀 |
| internal/mcp/http.go | 152 | 176 | +24 | 本地膨胀 |
| internal/web/keys.go | 167 | 189 | +22 | 本地膨胀 |
| internal/auth/cache.go | 298 | 316 | +18 | 本地膨胀 |
| internal/web/benchmark_http.go | 100 | 114 | +14 | 本地膨胀 |
| internal/chathub/wire_capture.go | 130 | 140 | +10 | 本地膨胀 |
| internal/chathub/client.go | 827 | 816 | -11 | 本地缺失 |
| internal/web/server.go | 1948 | 2039 | +91 | 本地膨胀 |
| **internal/web/router_history_trim.go** | **99** | **50** | **-49** | **本地缺失** |
| **internal/web/errors.go** | **113** | **51** | **-62** | **本地缺失** |
| **internal/web/tool_response.go** | **76** | **53** | **-23** | **本地缺失** |
| **internal/web/diag.go** | **348** | **254** | **-94** | **本地缺失** |

两个方向都值得注意：

- **本地膨胀**：多出的行可能是虚构实现（`stream_text.go` 已确证）。
  `toolloop.go` 本地是 APK 的 4.2 倍，`images.go` 3.1 倍，最可疑。
- **本地缺失**：APK 有更多代码而本地没有，说明恢复不完整。
  `diag.go` 缺 94 行、`errors.go` 缺 62 行最严重。

注意本表用的是"最大行号"，受文件内函数排布影响，只作粗筛信号，
不能单独作为判据。

## 结论四：待恢复函数（49 个）

完整清单见 `apk-missing-funcs-2026-08-19.txt`。按文件分组：

```
internal/web/model_tool_router.go   9  balancedArgs buildValidatedCall callNameAndArgs
                                       extractJSONValue oversizedShellBody
                                       parseCallToolDirective parseToolDecisionJSON
                                       stripCodeFence toolCallItems
internal/web/agent_ledger.go        6  agentLedger.CanContinue agentLedger.RouterContext
                                       agentLedger.hasCompleted shouldSuppressCompletedCall
                                       toolArgumentsJSON toolLooksObservational
internal/web/benchmark.go           6  (*Server).benchChat (*Server).callOwnChatCompletions
                                       (*Server).recordBenchUsage (*Server).runBenchTask
                                       (*Server).startBenchmark gradeBenchTask
cmd/server (net_mobile.go 等)       6  main configureResolver configureTLSRoots
                                       fileHasData parseDNSServers systemResolverUsable
internal/web/errors.go              4  (*Server).turnContext (*Server).turnCtx
                                       classifyUpstream upstreamStageError
internal/web/benchmark_cases.go     3  gradeDebug gradeIntervals gradeRefactor
internal/web/session_resolver.go    3  [已完成] contextSimilarity jaccardSimilarity tokenize
internal/outbound/proxy.go          3  httpsProxyDialer.DialContext
                                       socksContextDialer.DialContext wsWriteBufferBytes
internal/web/fenced_tools.go        2  commandProperty shellArgs
internal/web/protocol_compat.go     2  anthropicRequest.openAI responsesRequest.openAI
其余单项                            …  见清单文件
```

### 已推翻的错误前提

- APK 中**不存在** `(*gradeReport).check`。此前"必须先恢复 check
  才能重写 gradeInventory"的判断不成立。
- `runBenchTask` / `startBenchmark` 是 `*Server` 方法，不是自由函数。
- `benchmark_cases.go` 实际有 **8** 个评分器，除已知 5 个外还有
  `gradeDebug`、`gradeRefactor`、`gradeIntervals`。
- 历史记录的"10 个缺失文件"清单已失效，那 10 个文件现均存在。

## 当前验证状态

```
go build ./...                                          通过
go vet ./...                                            通过，无告警
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build ./...   通过
go test ./internal/{auth,chathub,mcp,outbound}           全部 ok
go test ./internal/web                                  2 项 FAIL（见结论二·3）
```

## 建议处置顺序

1. 定性 11 个 A 类文件：逐个判断是上游遗留、二次开发新增，还是虚构。
   优先 `public_identity.go`（27 个符号）与 `account_health.go`（11 个）。
2. 处置 `stream_text.go`：把三个拆出的函数合回 `flushStreamText`，
   目标行段对齐 APK 的 23-90。
3. 补齐"本地缺失"的四个文件：`diag.go`、`errors.go`、
   `tool_response.go`、`router_history_trim.go`。
4. 审查"本地膨胀"最严重的 `toolloop.go`（4.2 倍）与 `images.go`（3.1 倍）。
5. 对那 2 项前端测试做定性处置（删除或标记为已知偏差），
   不要伪造 HTML 文件。

## 证据文件

```
audit-results/apk-pclntab-project-2026-08-19.txt   APK 侧 81 文件 / 函数全表
audit-results/apk-missing-funcs-2026-08-19.txt     49 个待恢复函数
audit-work/apk2/apk-sym.txt                        APK 顶层符号 447
audit-work/apk2/apk-sym-with-inline.txt            穿透内联后 783
audit-work/apk2/local-sym-with-inline.txt          本地产物 821
audit-work/apk2/SUSPECT.txt                        L1 差集 244
audit-work/apk2/REAL-SUSPECT.txt                   L2 收敛 90
audit-work/apk2/REAL-SUSPECT-B.txt                 L3 收敛 50
audit-work/apk2/VERDICT.tsv                        行段规模对照
```

## 局限说明

`.text` 被 strip，无法做字节级分析，因此：

- 无法提取具体常量（阈值、超时、缓冲区大小等）
- 无法验证函数内部控制流细节
- 上一轮 `matchSimilarLocked` 使用的相似度阈值 `0.8` 是推断值，非实测

若需更高精度，需先修正 pclntab 到节地址的映射偏移
（当前函数地址 `0x1ce9300` 落在 `.text` 范围 `0x11000-0x37a814` 之外），
再做字节级反汇编。
