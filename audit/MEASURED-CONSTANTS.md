# 从 APK 实测的常量

工具：`tools/apktool`。地址映射修复后才可获取，此前只能推断。

## internal/web/session_resolver.go

### Resolve (APK 249-350, 3136 bytes)

字符串字面量（按代码顺序）：

| 偏移 | 长度 | 值 |
|---|---|---|
| +0x02a0 | 23 | `M365_CONTEXT_SIMILARITY` |
| +0x0478 | 17 | `context_prefix_%d` |
| +0x0ab8 | 20 | `context_similar_%.2f` |

相似度阈值默认值：**0.6**

追踪链：`+0x02b0 ADRP x27,0x5be000` → `+0x02b4 LDR d0,[x27,#1232]`
→ `0x5be4d0` → `raw=0x3fe3333333333333` → `f64=0.6`

邻近常量：`0x5be4c8` = 0.4，`0x5be4d8` = 2.0

### 对上一轮恢复的修正要点

`13d35c4` 提交的实现有三处与 APK 不符：

1. 格式串应为 `context_similar_%.2f`（小数），我写成了 `%d`（百分比整数）
2. 阈值应从环境变量 `M365_CONTEXT_SIMILARITY` 读取，默认 0.6；
   我硬编码为 0.8
3. APK 中**不存在** `matchSimilarLocked` 方法。
   session_resolver.go 的 APK 函数表在 246（tokenize 结束）与
   249（Resolve 开始）之间无空隙，相似度筛选逻辑写在 Resolve 体内。
   我新增该方法属于虚构。

`contextSimilarity` / `jaccardSimilarity` / `tokenize` 三个函数本身
存在于 APK，行段吻合，这部分恢复方向正确。

## internal/web/benchmark_cases.go

### gradeInventory (APK 35-680, 3568 bytes)

完整字符串字面量见 `graders-strings-2026-08-19.txt`。要点：

```
"inventory.py 存在"
"docstring 未被删除"          "保留原 docstring"
"Inventory / StockError"
"if qty < 1"  "if qty <= 0"  "if not isinstance(qty, int) or qty < 1"
"def add(self, sku, qty): if qty < 0"     <- 原始缺陷形态
"qty < 1 或 qty <= 0"
"def reserve"  "def release"  "raise"
"缺陷2 失败的 reserve 不写 trail"   "trail 先于校验"
"reserve 对未知 sku 抛 KeyError"
"available("  "self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)"
"缺陷4 reserve 依据可用量而非在库量"  "仍只比较 on_hand"  "扣除已预留后再比较"
"def available"
"缺陷5 release 后 reserved 不为负"  "未做下界保护"
"max(0"  "if result < 0"
"available 不返回负数"  "下界为 0"
```

此前只能从中文诊断串反推规则，现可按代码顺序与长度精确对齐每个
检查项及其接受的修复形态。

注意 APK 的 gradeInventory 是 3568 字节、行段 35-680，规模远大于
仓库现有实现，说明检查项数量与粒度都更细。
