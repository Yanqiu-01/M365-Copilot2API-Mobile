package web

import "testing"

func TestOriginalInventoryStillScoresLow(t *testing.T) {
	p, tot, f := gradeInventory(map[string]string{"inventory.py": benchInventorySource})
	t.Logf("原始文件 %d/%d 失败=%v", p, tot, f)
	if p >= 5 {
		t.Errorf("原始缺陷文件得分过高（%d/%d），判定被放宽过度", p, tot)
	}
	// 五处缺陷都应被检出
	want := map[string]bool{
		"缺陷1 add 拒绝 qty=0":           false,
		"缺陷2 失败操作不写入 trail":          false,
		"未见 KeyError":                false,
		"预留未依据可用量":                   false,
		"缺陷5 release 后 reserved 不为负": false,
	}
	for _, x := range f {
		if _, ok := want[x]; ok {
			want[x] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("未检出缺陷: %s", k)
		}
	}
}
