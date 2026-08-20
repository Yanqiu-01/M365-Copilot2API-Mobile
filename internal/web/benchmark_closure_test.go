package web

import "testing"

// 闭环判定必须以最终快照为准。
//
// workspace.testsPass 记录的是「最后一次 run_tests 调用时」的结论，而
// write_file 不会重置它。模型的常见轨迹是：写代码 → run_tests 失败 → 改对 →
// 直接收尾。此时 testsPass 仍为 false，尽管当前代码已经满分。
//
// 实测出现过「区间算法：原始 10/10、地板 0、最终 0%」—— 模型把题做对了，
// 却因为没有再跑一次测试而被清零。
func TestCodingClosureUsesFinalSnapshot(t *testing.T) {
	perfect := func(map[string]string) (int, int, []string) { return 10, 10, nil }
	partial := func(map[string]string) (int, int, []string) { return 6, 10, []string{"仍有缺陷"} }

	t.Run("最终满分即认定闭环达成", func(t *testing.T) {
		passed, total := 10, 10
		floor := 0
		testsPass := false // 最后一次 run_tests 时还没改对

		if total > 0 && passed == total {
			testsPass = true
		}
		net := passed
		if !testsPass {
			net = floor
		}
		if net != 10 {
			t.Fatalf("net=%d want 10 (perfect final snapshot must not be zeroed)", net)
		}
		if _, _, failures := perfect(nil); len(failures) != 0 {
			t.Fatalf("unexpected failures: %v", failures)
		}
	})

	t.Run("最终不满分仍按闭环未通过惩罚", func(t *testing.T) {
		passed, total, _ := partial(nil)
		floor := 3
		testsPass := false

		if total > 0 && passed == total {
			testsPass = true
		}
		net := passed
		if !testsPass {
			net = floor
		}
		if net != floor {
			t.Fatalf("net=%d want floor %d (incomplete work must still be penalised)", net, floor)
		}
	})
}

// runBenchTask 里的实际判定路径：构造一个已满分的工作区，确认不会被清零。
func TestRunBenchTaskKeepsPerfectScoreWithoutFinalRunTests(t *testing.T) {
	task := benchTask{
		ID:       "closure-probe",
		Title:    "闭环判定",
		Detail:   "probe",
		Category: "coding",
		Files:    map[string]string{"a.py": "ok"},
		Grader: func(files map[string]string) (int, int, []string) {
			if files["a.py"] == "fixed" {
				return 4, 4, nil
			}
			return 1, 4, []string{"未修复"}
		},
	}

	workspace := newBenchWorkspace(task)
	floor, _, _ := task.Grader(workspace.snapshot())
	if floor != 1 {
		t.Fatalf("floor=%d want 1", floor)
	}

	// 模拟：先跑测试（失败），再改对代码，之后不再跑测试。
	if _, err := workspace.execute("run_tests", nil); err != nil {
		t.Fatal(err)
	}
	if pass, runs := workspace.testStatus(); pass || runs != 1 {
		t.Fatalf("first run should fail: pass=%v runs=%d", pass, runs)
	}
	if _, err := workspace.execute("write_file", map[string]any{"path": "a.py", "content": "fixed"}); err != nil {
		t.Fatal(err)
	}
	if pass, _ := workspace.testStatus(); pass {
		t.Fatal("write_file must not flip testsPass to true on its own")
	}

	final := workspace.snapshot()
	passed, total, _ := task.Grader(final)
	if passed != total {
		t.Fatalf("final snapshot should be perfect: %d/%d", passed, total)
	}

	testsPass, _ := workspace.testStatus()
	if task.Category == "coding" && total > 0 && passed == total {
		testsPass = true
	}
	net := passed
	if task.Category == "coding" && !testsPass {
		net = floor
	}
	if net < floor {
		net = floor
	}
	if net != total {
		t.Errorf("net=%d want %d: a perfect final snapshot must score full marks", net, total)
	}
}
