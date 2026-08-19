# M365 网关 v3 修复版

**这个版本包含实际的代码修复**，与 v2（仅改包名）不同。

| | 原版 | v2 | v3 |
|---|---|---|---|
| 包名 | `com.m365.gateway` | `com.m365.gateway2` | `com.m365.gateway3` |
| 应用名 | M365 Copilot 网关 | M365 网关 v2 | M365 网关 v3 修复版 |
| `libm365.so` | 原始 | 原始 | **恢复源码重新编译** |
| 含代码修复 | — | 否 | **是** |

三者包名互不相同，可同时安装。

## 包含的修复

1. **思考时长显示为 0**（提交 f618502）
   思考内容此前只在完成帧被一次性补发，客户端看到首末增量几乎同时
   抵达，时长算成 0。改为逐帧实时推送（新增 `reasoningPump`），
   取材范围与兜底路径统一。
   顺带修好一个更广的漏洞：`candidateMessages` 未下钻
   `arguments[].messages`，而这是最主流的帧形态。

2. **评测重复计入用量**（提交 379d4a4）
   评测经自调用走 `openaiChat`，与 `recordBenchUsage` 各记一次，
   同一次调用被统计两遍。现由 `X-M365-Internal-Call` 头标记跳过。

3. **首次实现 `M365_DNS`**（提交 7bd0974）
   Java 侧一直注入该变量但无人消费。现在系统 resolver 不可用时
   会启用自定义 DNS（默认 223.5.5.5 / 119.29.29.29 / 1.1.1.1 / 8.8.8.8）。
   这可能改善部分网络环境下的域名解析失败。

4. **首次实现 `M365_PROMPT`** 与 Android 根证书路径探测（同上提交）

5. 清除对话详情页虚构链（提交 633cea6）：
   `/conversation` 页面与 `/api/m365/conversations/detail` 在原版中
   本就不存在，此前是恢复过程中被误加的。

## 二进制兼容性验证

新编译的 `.so` 与原版对照：

```
Type          DYN (PIE)              一致
Machine       AArch64                一致
Entry point   0x86730                完全相同
INTERP        /system/bin/linker64   一致
导出符号      1 个（main.main）      一致
NEEDED        无（静态链接）         一致
```

原版并非 JNI 库，而是被 `GatewayService` 以 `ProcessBuilder`
启动的子进程。恢复的 `cmd/server` 正是同一形态，
用 `GOOS=android GOARCH=arm64 -buildmode=pie` 编译即可直接替换。

体积 32.8 MB vs 原版 29.3 MB：原版用了 `-s -w` 裁剪符号表
（加上后为 29.4 MB，仅差 65 KB）。v3 保留符号表以便日后取证比对。

## 已做的验证

```
go build ./...              通过
go vet ./...                通过
go test ./...               6 个包全部通过
android/arm64 pie 交叉编译   通过
本地冒烟测试                服务正常启动，/ 与 /login 返回 200
签名                        v2 + v3 方案通过
libcloudflared.so           未改动
APK 条目数                  22 → 22，无缺失
```

## 未经验证的部分

**思考时长的修复只在离线测试中验证过。** 取材逻辑是从既有两条代码
路径合并推导的，若 ChatHub 还有未覆盖的帧结构，可能仍有遗漏。

请实测确认。若仍显示 0，开启 `M365_DEBUG_LOG` 抓一段
`[trace:ws] frame_len=...` 日志，可据真实帧结构进一步定位。

**`M365_DNS` 的行为变化需留意。** 此前该变量无效，现在生效了。
若你所在网络依赖特定 DNS，而系统 `/etc/resolv.conf` 可用，
代码不会接管；只在系统 resolver 不可用时才启用内置列表。

## 安装

```
adb install M365-Gateway-v3-fixed.apk
```

首次需重新配置账号与 API Key（Android 按包名隔离存储）。

## 密钥库

`m365-gateway-v2.jks`（v2 与 v3 共用）
- 别名 `m365v2`，口令 `[REDACTED]`
- 证书 SHA-256 `8199a4c91043d857ffc88303ab2949639c1337455fd0ffd0891d50d88d27b418`

**请备份。** 后续更新必须用同一密钥，否则无法覆盖安装。
