package main

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"os"
	"strings"
	"time"
)

// 本文件解决 Android 上的两个环境差异：
//
//  1. 应用进程读不到可用的 /etc/resolv.conf，Go 的纯 Go resolver 会解析失败；
//  2. 系统根证书不在 glibc 的默认位置，crypto/x509 找不到 CA。
//
// APK 证据（tools/apktool，cmd/server/net_mobile.go）：
//
//	init.0                25       调 configureResolver 与 configureTLSRoots
//	configureResolver     30       含 func1 @42
//	systemResolverUsable  64
//	configureTLSRoots     88
//	fileHasData          130
//	parseDNSServers      141       含 func1 @144
//
// Java 侧（GatewayService.smali）以 ProcessBuilder 启动本进程，并注入
// M365_DNS 与 SSL_CERT_FILE / SSL_CERT_DIR，正是此处消费的变量。
func init() {
	configureResolver()
	configureTLSRoots()
}

// configureResolver 在系统 resolver 不可用时安装自定义 DNS。
//
// APK 证据：configureResolver 调用图为
// os.Getenv → parseDNSServers → systemResolverUsable → runtime.newobject，
// 其中 newobject 构造 net.Dialer；+0x0018 引用 "M365_DNS"(8)。
// 默认服务器取自 rodata：223.5.5.5:53、119.29.29.29:53、1.1.1.1:53、8.8.8.8:53
// （与 Java 侧注入的 M365_DNS 默认值一致）。
func configureResolver() {
	servers := parseDNSServers(os.Getenv("M365_DNS"))
	if len(servers) == 0 {
		servers = []string{"223.5.5.5:53", "119.29.29.29:53", "1.1.1.1:53", "8.8.8.8:53"}
	}
	// 系统 resolver 可用时不接管，避免干扰用户自定的 DNS 配置。
	if systemResolverUsable() {
		return
	}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 5 * time.Second}
			var lastErr error
			for _, server := range servers {
				conn, err := dialer.DialContext(ctx, network, server)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
}

// systemResolverUsable 判定 /etc/resolv.conf 是否含可用的 nameserver。
//
// APK 证据：调用图 os.Getenv → concatstring2 → memequal → os.OpenFile →
// bufio.Scanner.Scan → strings.TrimSpace → strings.Fields；
// +0x0040 引用 "/etc/resolv.conf"(16)。
func systemResolverUsable() bool {
	file, err := os.OpenFile("/etc/resolv.conf", os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	usable := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			usable = true
			break
		}
	}
	// APK 的调用图含 os.(*file).close 但无 deferwrap，说明显式关闭而非 defer。
	_ = file.Close()
	return usable
}

// configureTLSRoots 定位 Android 的系统根证书并导出给 crypto/x509。
//
// APK 证据：调用图 os.Getenv → fileHasData → concatstring2 → os.Setenv →
// os.Stat → growslice → strings.Join。
// Java 侧注入的 SSL_CERT_DIR 为
// /apex/com.android.conscrypt/cacerts:/system/etc/security/cacerts:/data/misc/keychain/certs-added，
// 此处按存在性筛选后再写回环境变量。
func configureTLSRoots() {
	// 已由外部显式指定且文件有内容时不覆盖。
	if fileHasData("SSL_CERT_FILE") {
		return
	}
	candidates := []string{
		"/apex/com.android.conscrypt/cacerts",
		"/system/etc/security/cacerts",
		"/data/misc/keychain/certs-added",
	}
	if existing := strings.TrimSpace(os.Getenv("SSL_CERT_DIR")); existing != "" {
		candidates = append(strings.Split(existing, ":"), candidates...)
	}
	var usable []string
	seen := map[string]bool{}
	for _, dir := range candidates {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			usable = append(usable, dir)
		}
	}
	if len(usable) > 0 {
		_ = os.Setenv("SSL_CERT_DIR", strings.Join(usable, ":"))
	}
}

// fileHasData 判定指定环境变量所指的文件存在且含实际内容。
//
// APK 证据：调用图 os.Getenv → os.ReadFile → bytes.Index；
// +0x0030 的 MOVZ #25903 是被搜索的字节序列，用于确认文件不是空壳。
func fileHasData(envKey string) bool {
	path := strings.TrimSpace(os.Getenv(envKey))
	if path == "" {
		return false
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 {
		return false
	}
	// 证书文件必须含 PEM 头，否则视为无效。
	return bytes.Index(body, []byte("-----")) >= 0
}

// parseDNSServers 把逗号或空白分隔的服务器列表规范化为 host:port。
//
// APK 证据：调用图 strings.FieldsFunc → strings.TrimSpace →
// net.SplitHostPort → bytealg.IndexByteString → concatstring4 →
// concatstring3 → growslice。IndexByteString 用于识别 IPv6 字面量，
// 两个 concatstring 分别对应 "[v6]:port" 与 "v4:port" 两种拼接。
func parseDNSServers(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	var out []string
	for _, field := range fields {
		server := strings.TrimSpace(field)
		if server == "" {
			continue
		}
		// 已带端口则原样保留。
		if _, _, err := net.SplitHostPort(server); err == nil {
			out = append(out, server)
			continue
		}
		// 裸 IPv6 需要方括号包裹后再补端口。
		if strings.IndexByte(server, ':') >= 0 {
			out = append(out, "["+server+"]"+":"+"53")
			continue
		}
		out = append(out, server+":"+"53")
	}
	return out
}
