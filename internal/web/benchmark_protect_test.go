package web

import (
	"strings"
	"testing"
)

// gradeBenchTask 的语义完全由 APK 机器码约束：
// 遍历受保护 map、cleanBenchPath 规范化、在提交 map 中查同名项、
// 先比长度再 memequal 比内容，不一致则返回 "受保护输入被修改: " + 原始键名。
func TestGradeBenchTaskDetectsProtectedMutation(t *testing.T) {
	protected := map[string]string{
		"inventory.py":      "class Inventory:\n    pass\n",
		"tests/test_inv.py": "def test_ok():\n    assert True\n",
	}

	t.Run("unchanged passes", func(t *testing.T) {
		submitted := map[string]string{
			"inventory.py":      protected["inventory.py"],
			"tests/test_inv.py": protected["tests/test_inv.py"],
			"notes.md":          "extra file is fine",
		}
		if got := gradeBenchTask(protected, submitted); got != "" {
			t.Fatalf("unchanged protected inputs must pass, got %q", got)
		}
	})

	t.Run("content edited is reported", func(t *testing.T) {
		submitted := map[string]string{
			"inventory.py":      "class Inventory:\n    HACKED\n",
			"tests/test_inv.py": protected["tests/test_inv.py"],
		}
		got := gradeBenchTask(protected, submitted)
		if !strings.HasPrefix(got, "受保护输入被修改: ") {
			t.Fatalf("expected mutation prefix, got %q", got)
		}
		if !strings.HasSuffix(got, "inventory.py") {
			t.Fatalf("expected offending file name in %q", got)
		}
	})

	t.Run("deleted is reported", func(t *testing.T) {
		submitted := map[string]string{
			"inventory.py": protected["inventory.py"],
		}
		got := gradeBenchTask(protected, submitted)
		if !strings.Contains(got, "tests/test_inv.py") {
			t.Fatalf("missing protected file must be reported, got %q", got)
		}
	})

	// 长度相同但内容不同：必须走到 memequal 分支而不是被长度检查放过。
	t.Run("same length different content", func(t *testing.T) {
		protectedOne := map[string]string{"a.txt": "abcd"}
		submitted := map[string]string{"a.txt": "abce"}
		if got := gradeBenchTask(protectedOne, submitted); got == "" {
			t.Fatal("equal-length but different content must be reported")
		}
	})

	// 空的受保护集合恒通过。
	t.Run("empty protected set passes", func(t *testing.T) {
		if got := gradeBenchTask(map[string]string{}, map[string]string{"x": "y"}); got != "" {
			t.Fatalf("empty protected set must pass, got %q", got)
		}
		if got := gradeBenchTask(nil, nil); got != "" {
			t.Fatalf("nil maps must pass, got %q", got)
		}
	})
}

// APK 在查找前对键调用 cleanBenchPath，因此受保护键的写法可与提交键的
// 规范形式不同（如 Windows 分隔符），仍应匹配上。
func TestGradeBenchTaskNormalizesProtectedKeys(t *testing.T) {
	protected := map[string]string{`pkg\mod.py`: "x = 1\n"}
	submitted := map[string]string{"pkg/mod.py": "x = 1\n"}
	if got := gradeBenchTask(protected, submitted); got != "" {
		t.Fatalf("normalized key must match, got %q", got)
	}

	// 规范化后内容不符时，诊断里用的是原始键名。
	mutated := map[string]string{"pkg/mod.py": "x = 2\n"}
	got := gradeBenchTask(protected, mutated)
	if !strings.Contains(got, `pkg\mod.py`) {
		t.Fatalf("diagnostic should carry the original key, got %q", got)
	}
}

// cleanBenchPath 拒绝的键被跳过，不应误报。
func TestGradeBenchTaskSkipsInvalidProtectedKeys(t *testing.T) {
	for _, bad := range []string{"", "/etc/passwd", "../escape.py", ".."} {
		protected := map[string]string{bad: "whatever"}
		if got := gradeBenchTask(protected, map[string]string{}); got != "" {
			t.Fatalf("invalid protected key %q must be skipped, got %q", bad, got)
		}
	}
}
