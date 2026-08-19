# apktool — APK 源码恢复取证工具

在符号表被 strip 的 Go ELF 上做字节级取证。为 `m365-copilot2api`
从 APK 恢复源码而写，但对任何 Go 编译的 ELF 都适用。

## 为什么需要它

`libm365.so` 的 `.text` 已被 strip，`go tool objdump` 直接报
`no symbol section`。此前的审计只能依赖 pclntab 的函数名与行号，
拿不到常量、字符串、控制流，导致多处恢复只能靠推断。

## 已修复的关键缺陷

`gosym.NewLineTable(data, addr)` 的第二个参数必须是 **text 段起始地址**，
不是 pclntab 节自身的地址。此前传入了 `sec.Addr`（pclntab 地址
`0x199b660`），导致全部 7790 个函数入口越界，无法读取机器码。

真实 `textStart` 记录在 pclntab 头部偏移 `8+2*ptrSize` 处，
本工具直接读取，并做落节校验。

修复效果：

```
修复前  sanity: 0 in-range, 7790 out-of-range     字节级分析不可用
修复后  sanity: 7790 in-range, 0 out-of-range     可用
```

副作用：本项目可见函数数从 447 升到 783，因为内联归属也随之正确。

## 构建

```sh
export PATH=/workspace/toolchain/go1.23/bin:$PATH
cd /workspace/audit-work/apktool && go build -o apktool .
```

需 Go 1.23（与 APK 构建版本 go1.23.12 一致）。

## 子命令

```
info    <bin>                     pclntab 头部与自洽性检查
files   <bin>                     列出本项目源文件
funcs   <bin> [file]              函数与行段（含地址、字节大小）
inline  <bin> <funcSubstr>        内联树：该函数内联了哪些项目内函数
strings <bin> <funcSubstr>        字符串字面量（按代码顺序，带长度）与立即数
diff    <apk> <local>             符号差集
span    <apk> <local> [file...]   行段规模对照
```

`funcSubstr` 是子串匹配，非正则。

## 典型用法

确认地址映射正常：

```sh
./apktool info apk2/lib/arm64-v8a/libm365.so
```

取某函数的字符串证据（这是恢复评分器逻辑的主要依据）：

```sh
./apktool strings apk2/lib/arm64-v8a/libm365.so gradeInventory
```

判断某函数是被内联还是真不存在：

```sh
./apktool inline apk2/lib/arm64-v8a/libm365.so flushStreamText
```

对照本地恢复进度：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/mine.elf ./cmd/server
./apktool span apk2/lib/arm64-v8a/libm365.so /tmp/mine.elf
./apktool diff apk2/lib/arm64-v8a/libm365.so /tmp/mine.elf
```

## 包结构

```
binimg/    ELF 加载、pclntab 头部解析、按虚拟地址读内存
symview/   符号、行段、内联树的查询视图
arm64/     ADRP+ADD 地址恢复、MOVZ/MOVK 立即数、字符串引用配对
main.go    CLI
```

`arm64` 不是通用反汇编器，只覆盖 Go 代码生成中稳定出现的模式。
遇到不认识的指令会跳过而非报错。

## 方法论：三级收敛

单纯比较符号名会产生大量假阳性 —— 小函数被内联、无调用点被链接器
裁剪，都不出现在顶层符号表。故：

| 阶段 | 判据 | 本项目结果 |
|---|---|---|
| L0 | APK 顶层符号 vs 仓库声明，直接差集 | 244 |
| L1 | 穿透 inline tree | 244 |
| L2 | **对照本地同参数编译产物取交集** | 90 |
| L3 | 剔除整文件不在 APK 者 | 50 |

L2 是关键：若某函数在本地产物中**也**不可见，说明它被内联或消除，
其在 APK 中不可见属正常现象，不能判为虚构。该步排除 154 个假阳性。

判定某函数「APK 中不存在」需要三条证据同时成立：

1. 不在 APK 顶层符号表
2. 不在任何 APK 函数的 inline tree 中
3. 在本地同参数产物中可见（排除被优化掉的可能）

再辅以行段占用检查：若 APK 侧相邻函数的行段已首尾相接、覆盖整个
文件，则没有多余函数的容身之处。

## 局限

- `arm64` 包只识别少数指令模式，复杂寻址会漏
- 浮点立即数若经由全局变量加载，需手工跟 `ADRP+LDR` 链
- 无法恢复控制流图，判断分支结构仍需人工读指令
