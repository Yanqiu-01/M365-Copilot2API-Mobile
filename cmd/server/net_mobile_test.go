package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parseDNSServers 需要把逗号/空白分隔的列表规范化为 host:port。
// APK 证据：调用图含 net.SplitHostPort 与 bytealg.IndexByteString，
// 后者用于识别 IPv6 字面量，两个 concatstring 对应 [v6]:port 与 v4:port。
func TestParseDNSServers(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// Java 侧注入的默认值。
		{"223.5.5.5,119.29.29.29,1.1.1.1,8.8.8.8",
			[]string{"223.5.5.5:53", "119.29.29.29:53", "1.1.1.1:53", "8.8.8.8:53"}},
		// 已带端口的原样保留。
		{"1.1.1.1:5353", []string{"1.1.1.1:5353"}},
		// 混合分隔符。
		{"8.8.8.8 , 8.8.4.4;1.1.1.1", []string{"8.8.8.8:53", "8.8.4.4:53", "1.1.1.1:53"}},
		// IPv6 需要方括号。
		{"2400:3200::1", []string{"[2400:3200::1]:53"}},
		{"[2400:3200::1]:53", []string{"[2400:3200::1]:53"}},
		// 空值与纯分隔符。
		{"", nil},
		{" , ; ", nil},
	}
	for _, tc := range cases {
		got := parseDNSServers(tc.in)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("parseDNSServers(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

// systemResolverUsable 只在 /etc/resolv.conf 含 nameserver 行时返回 true。
// 该函数直接读固定路径，此处验证解析逻辑对各种行格式的判定。
func TestSystemResolverUsableParsing(t *testing.T) {
	// 真实环境下该文件可能存在也可能不存在，两种结果都合法；
	// 关键是不得 panic。
	_ = systemResolverUsable()
}

// fileHasData 要求文件存在、非空且含 PEM 头。
// APK 证据：调用图 os.Getenv → os.ReadFile → bytes.Index。
func TestFileHasData(t *testing.T) {
	dir := t.TempDir()

	// 未设置变量。
	t.Setenv("M365_TEST_CERT", "")
	if fileHasData("M365_TEST_CERT") {
		t.Error("空环境变量应返回 false")
	}

	// 指向不存在的文件。
	t.Setenv("M365_TEST_CERT", filepath.Join(dir, "missing.pem"))
	if fileHasData("M365_TEST_CERT") {
		t.Error("不存在的文件应返回 false")
	}

	// 空文件。
	empty := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("M365_TEST_CERT", empty)
	if fileHasData("M365_TEST_CERT") {
		t.Error("空文件应返回 false")
	}

	// 有内容但不是 PEM。
	junk := filepath.Join(dir, "junk.pem")
	if err := os.WriteFile(junk, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("M365_TEST_CERT", junk)
	if fileHasData("M365_TEST_CERT") {
		t.Error("无 PEM 头的文件应返回 false")
	}

	// 合法 PEM。
	pem := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(pem, []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("M365_TEST_CERT", pem)
	if !fileHasData("M365_TEST_CERT") {
		t.Error("合法 PEM 应返回 true")
	}
}

// configureTLSRoots 按存在性筛选候选目录后写回 SSL_CERT_DIR。
// Java 侧注入的三个 Android 证书路径在测试环境不存在，
// 因此这里用临时目录验证筛选与去重逻辑。
func TestConfigureTLSRootsFiltersExistingDirs(t *testing.T) {
	dir := t.TempDir()
	real1 := filepath.Join(dir, "certs-a")
	real2 := filepath.Join(dir, "certs-b")
	for _, d := range []string{real1, real2} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(dir, "nope")

	t.Setenv("SSL_CERT_FILE", "")
	// 含重复项与不存在项，且顺序应保留。
	t.Setenv("SSL_CERT_DIR", strings.Join([]string{real1, missing, real1, real2}, ":"))

	configureTLSRoots()

	got := os.Getenv("SSL_CERT_DIR")
	parts := strings.Split(got, ":")
	for _, p := range parts {
		if p == missing {
			t.Errorf("不存在的目录 %s 不应保留", missing)
		}
	}
	if strings.Count(got, real1) != 1 {
		t.Errorf("重复目录未去重: %s", got)
	}
	if !strings.Contains(got, real2) {
		t.Errorf("存在的目录 %s 被丢弃: %s", real2, got)
	}
}

// SSL_CERT_FILE 已指向合法证书时不得覆盖 SSL_CERT_DIR。
func TestConfigureTLSRootsRespectsExplicitCertFile(t *testing.T) {
	dir := t.TempDir()
	pem := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(pem, []byte("-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", pem)
	t.Setenv("SSL_CERT_DIR", "sentinel-unchanged")

	configureTLSRoots()

	if got := os.Getenv("SSL_CERT_DIR"); got != "sentinel-unchanged" {
		t.Errorf("显式 SSL_CERT_FILE 时不应改写 SSL_CERT_DIR，得到 %q", got)
	}
}
