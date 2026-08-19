# APK 源码恢复进度

最后更新：2026-08-19
基线 APK：`delivery/original/base.apk`
sha256：`374ab2e2e0a456cebcdbe5f384b3f2774e84b5950985659f3f929fc2028b5029`
APK 构建：go1.23.12，入口 `cmd/server`，
`-trimpath -buildmode=exe CGO_ENABLED=0 GOOS=android GOARCH=arm64 GOARM64=v8.0`

## 工具链

Go 1.23.12 装于 `/workspace/toolchain/go1.23`。
可用镜像仅 `mirrors.nju.edu.cn/golang`（阿里/goproxy.cn/dl.google.com/
golang.google.cn 不可达，USTC 403，清华/华为/腾讯无此文件）。
sha256 已与 `go.dev/dl` 官方 JSON 逐位核对一致。

取证工具：`tools/apktool`（详见其 README.md）。
辅助脚本（未纳入仓库，位于 `/workspace/audit-work/apk2/`）：
- `cg/` 调用图：`go run main.go <so> '<完整符号名>'`
- `dis/` 逐指令：`go run main.go <so> '<符号>' <起偏移> <止偏移>`
- `fl/` 读 rodata 字符串与全局 slice（每次按需改写）

## 提交序列

```
58282c4  recover: gradeDebug / gradeRefactor / gradeIntervals
6fce8e0  audit: 提取 benchTasks 八个任务产物（逐字节验证）
ad78f89  recover: benchChat / recordBenchUsage + agent_ledger 三项
f0b6634  recover: callOwnChatCompletions
b010d27  recover: gradeBenchTask
792be26  fix: 按实测证据修正 13d35c4 三处偏差
9b23e45  tools: apktool 工具链，修复 pclntab 地址映射
e02a07f  audit: 全量审计（三级收敛）
13d35c4  recover: 会话相似度兜底
6067982  revert: 撤销 47b4ee5 / 6529b07 破坏性改动
```

## benchmark.go 缺口进度

APK 共 767 行，本地曾仅 157 行。六个缺失函数：

| 函数 | APK 行段 | 字节 | 状态 |
|---|---|---|---|
| `callOwnChatCompletions` | 362-374 | 1200 | ✅ 行数精确 13=13 |
| `benchChat` | 378-410 | 1600 | ✅ 行数精确 33=33 |
| `recordBenchUsage` | 421-499 | 1968 | ✅ 语义完整，79 vs 51 |
| `gradeBenchTask` | 505-515 | 624 | ✅ 行数精确 11=11 |
| `runBenchTask` | 518-644 | 6000 | ⬜ 依赖 benchTasks 产物 |
| `startBenchmark` | 649-719 | 1184 | ⬜ 证据已备齐 |

## benchTasks 任务产物：已提取（audit/artifacts/）

APK `benchTasks`（64-95，1568 字节，32 行）内含完整任务产物，
本地同名函数此前只有 ID/Title/Detail/Category 元数据。

**八个产物已逐字节提取，长度与 MOVZ 标注全部精确吻合：**

| 文件 | 长度 | 值地址 | 归属任务 |
|---|---|---|---|
| `inventory.py` | 1724 | `0x522000+469` | bugfix |
| `stats.py` | 531 | `0x51f000+722` | debug |
| `run_report.txt` | 387 | `0x51e000+3498` | debug |
| `users.py` | 544 | `0x51f000+1789` | refactor |
| `staff.py` | 552 | `0x51f000+2333` | refactor |
| `people.json` | 203 | `0x51e000+909` | refactor |
| `ledger.txt` | 144 | `0x51d000+2165` | ledger |
| `sales.csv` | 320 | `0x51e000+2854` | sales |

### 交叉验证成功

`sales.csv` 独立算出：north=80 south=80 east=70 total=230
topMonth=2026-02(115) —— 与 `gradeSales` 期望值、以及 rodata 常量池
`0x5be4e0`-`0x5be4f8` 的 70/80/100/115 完全一致。数据链闭合。

`inventory.py` 含完整 CONTRACT 与五个植入缺陷，与 `gradeInventory`
的检查项逐条对应（`if qty < 0` 而非 `>= 1`、trail 先于校验、
比较 `on_hand` 而非 available、release 不设下界）。

`ledger.txt` 十条操作中 4 条无效（TRANSFER B C 200 超额、
DEPOSIT C -10 负数、WITHDRAW C 0 零额、WITHDRAW B 80 超额），
与 `gradeLedger` 的「4 拒 6 应用」吻合。

`users.py` / `staff.py` 是近乎重复的两份实现，正是 refactor 任务
要合并的目标。

### 提取方法（重要）

`benchTasks` 中每个 map 项的模式为：
```
ADRP x4, <值页>  / ADD x4,x4,#<值偏移>  / STR x4,[x0,#0]   ← 前一项的值指针
ADRP x2, <键页>  / ADD x2,x2,#<键偏移>  / MOVZ x3,#<键长>
BL mapassign_faststr
MOVZ x1,#<值长>  / STR x1,[x0,#8]                          ← 当前项的值长度
```
值指针出现在**前一个** `STR x4,[x0,#0]`，值长度在 `mapassign` **之后**。
错位配对会导致内容截断（我最初把 `staff.py` 的 552 误配成 203）。

**注意**：`+0x007c` 等处 `ADRP x2,0x4b1000` 配 `MOVZ x1,#1724` 看似
是字符串，实为 Go 链接器按长度分桶的**字符串池**，池内含数十个
不相关字面量。不可整段当单个字符串使用。

### algorithm 任务无初始产物（已定论）

全项目 rodata 中 `"intervals.py"` 仅出现在字符串池内，
不存在对应的内容字面量。`gradeIntervals` 首个检查项为
「intervals.py 存在」，即要求从零创建。故八个产物即为全部。

### 仍缺

`benchTask` 结构体需扩展产物字段（Files / Protected / 评分器挂载），
须先确认 APK 侧字段布局，再补全 `benchTasks` 的八项任务定义。

## startBenchmark 已备齐的证据

```
本体 649-719 (1184B)
  调用图 Mutex.lockSlow / time.Now / time.Time.Format /
         Mutex.unlockSlow / fmt.Errorf / strings.TrimSpace
  +0x0310 "已有评测在运行"(21)
  +0x011c 时间格式 "20060102T150405Z"(16)
func1 666-743 (2688B)  主循环
  +0x00e0 "开始评测：模型 %s，思考强度 %s（编程占 %.0f%%，纯 Go 隐藏评分器）"(89)
  +0x07f8 "评测结束：编程 %.0f%%，推理 %.0f%%，加权总分 %.0f%%"(66)
  +0x0464 整数化字符串比较 "cancelled"
  +0x0528 "reasoning"(9) / +0x0558 "coding"(6)
  +0x0480 CMP #100 / +0x0534 CMP #6 / +0x0454 CMP #9
  调 benchTasks / benchmarkStore.update / runBenchTask /
    benchWeightedAverage / func1.1
func1.1 667-681 (528B)  每任务状态更新，调 benchmarkStore.update
func1.2/.3/.5 均单行 (112B)
func1.4 703-708 (336B)
```

## agent_ledger.go 进度

| 函数 | APK | 状态 |
|---|---|---|
| `toolArgumentsJSON` | 81-91 | ✅ 11 vs 13 |
| `toolLooksObservational` | 237-244 | ✅ 精确 8=8 |
| `shouldSuppressCompletedCall` | 247-251 | ✅ 精确 5=5 |
| `agentLedger.CanContinue` | 270-280 | ⬜ 本地已有，需核对 |
| `agentLedger.RouterContext` | 156-199 | ⬜ 本地已有，需核对 |
| `agentLedger.hasCompleted` | 214-221 | ⬜ 本地已有，需核对 |

`toolLooksObservational` 关键字表实测 22 项（全局 slice `0x1be6000+2656`）：
read list get search find fetch inspect stat status describe info
test check verify validate browser lookup diff log show view grep

## 实测常量汇总

| 位置 | 值 | 来源 |
|---|---|---|
| 相似度阈值默认 | 0.6 | `0x5be4d0` = `0x3fe3333333333333` |
| 相似度阈值上界 | 1.0 | `FMOV d1,#1.0` + `FCMP`/`B.LS` |
| 相似度环境变量 | `M365_CONTEXT_SIMILARITY` | Resolve `+0x02a0`(23) |
| matchedBy 格式 | `context_similar_%.2f` | Resolve `+0x0ab8`(20) |
| benchChat 超时 | 240s | `+0x0274` MOVZ+MOVK |
| 工具结果截断 | 300 | `+0x03e8` MOVZ #300 |
| 评测失败状态码 | 502 / 200 | `+0x0384`/`+0x0390` + CSEL |
| 编程/推理权重 | 0.60 / 0.40 | 本地 benchWeightedAverage 已有 |

邻近常量池 `0x5be4c0`-`0x5be4f8`：
0.01 / 0.4 / **0.6** / 2 / 70 / 80 / 100 / 115
（后四个是 gradeSales/gradeLedger 期望值，印证归属正确）

## 全局待恢复清单

`audit/apk-missing-funcs-2026-08-19.txt` 原列 49 项，已完成 13 项：
contextSimilarity / jaccardSimilarity / tokenize / gradeBenchTask /
callOwnChatCompletions / benchChat / recordBenchUsage /
toolArgumentsJSON / toolLooksObservational / shouldSuppressCompletedCall /
gradeDebug / gradeRefactor / gradeIntervals

`benchmark_cases.go` 的 8 个评分器已全部恢复。

剩余较大者：
- `internal/web/model_tool_router.go` 9 项
- `internal/web/errors.go` 4 项
- `cmd/server/net_mobile.go` 整个文件缺失（6 项）

## 双向缺口（`audit/span-2026-08-19.txt`）

本地缺失显著：
```
benchmark.go          767 vs 157   -610（进行中）
agent_ledger.go       336 vs 225   -111
diag.go               348 vs 254    -94
account_cooldown.go   200 vs 125    -75
errors.go             113 vs  51    -62
router_retry.go       142 vs 109    -33
wire_capture_diag.go   81 vs  39    -42
```

本地膨胀（疑虚构）：
```
toolloop.go    58 vs 244  4.2x
images.go     172 vs 535  3.1x
conversations.go 228 vs 341
codex_catalog.go 248 vs 324
```

已确证虚构并处置：
- `stream_text.go` 的 `declaredFencePrefix`/`completeFenceEnd`/`keepRuneTail`
  （APK flushStreamText 为 68 行单体，未内联任何项目内函数）—— 待清理
- `session_resolver.go` 的 `matchSuffixLocked`/`suffixMatchLen` —— 已删
- `matchSimilarLocked` —— 已删（我自己在 13d35c4 引入）

## 长期遗留

`internal/web` 有 2 项 FAIL，断言 `web/index.html` 的
`localeSelectionKey` 与 `web/conversation.html`。
APK `assets/web/` 只有 index.html / login.html / debug.html，
其 index.html 不含该标记，conversation.html 不存在。
属虚构断言，不伪造文件凑绿，待单独定性。

## 方法要点

判定"APK 中不存在"需三条同时成立：
1. 不在顶层符号表
2. 不在任何函数的 inline tree 中
3. 在本地同参数编译产物中可见（排除被优化掉）

再辅以行段缝隙检查：若 APK 相邻函数行段首尾相接，
则中间没有多余函数的容身空间。

死代码会被链接器裁剪，观察未接入函数的行段需用
`go test -c` 产物而非 `cmd/server`。

## 已知局限

`.text` 被 strip，`go tool objdump` 不可用。
`arm64` 包只识别 Go 代码生成的常见模式，复杂寻址会漏。
浮点常量若经全局变量加载需手工跟 `ADRP`+`LDR` 链。
无法恢复控制流图，分支结构仍需人工读指令。
本环境 ThreadSanitizer 不可用（unsupported VMA range），
`-race` 无法运行。
