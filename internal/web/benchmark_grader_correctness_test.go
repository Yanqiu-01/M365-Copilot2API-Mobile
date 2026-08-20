package web

import "testing"

func TestGradersAcceptCorrectSolutions(t *testing.T) {
	t.Run("debug 正确解（提前 return 后用 max）", func(t *testing.T) {
		src := `def summarize(rows):
    """Return {"count", "total", "mean", "max"} for a list of numbers."""
    count = len(rows)
    if count == 0:
        return {"count": 0, "total": 0, "mean": None, "max": None}
    total = sum(rows)
    return {"count": count, "total": total, "mean": round(total / count, 2), "max": max(rows)}


def format_report(rows):
    stats = summarize(rows)
    mean = stats["mean"]
    text = "None" if mean is None else f"{mean:.2f}"
    return f"count={stats['count']} total={stats['total']} mean={text}"
`
		p, tot, f := gradeDebug(map[string]string{"stats.py": src})
		t.Logf("得分 %d/%d  失败=%v", p, tot, f)
		if p != tot {
			t.Errorf("正确解应满分")
		}
	})

	t.Run("debug 正确解（条件化 max）", func(t *testing.T) {
		src := `def summarize(rows):
    """CONTRACT"""
    if not rows:
        return {"count": 0, "total": 0, "mean": None, "max": None}
    return {"count": len(rows), "total": sum(rows), "mean": round(sum(rows)/len(rows), 2), "max": max(rows) if rows else None}


def format_report(rows):
    s = summarize(rows)
    return f"count={s['count']} total={s['total']} mean={s['mean'] if s['mean'] is None else format(s['mean'], '.2f')}"
`
		p, tot, f := gradeDebug(map[string]string{"stats.py": src})
		t.Logf("得分 %d/%d  失败=%v", p, tot, f)
		if p != tot {
			t.Errorf("正确解应满分")
		}
	})

	t.Run("bugfix 正确解", func(t *testing.T) {
		src := `class StockError(Exception):
    pass


class Inventory:
    """CONTRACT
    add(sku, qty): qty must be >= 1
    reserve: must not write trail on failure; unknown sku raises KeyError
    release: reserved must never go negative
    """

    def __init__(self):
        self.on_hand = {}
        self.reserved = {}
        self.trail = []

    def add(self, sku, qty):
        if not isinstance(qty, int) or qty < 1:
            raise StockError("qty must be >= 1")
        self.on_hand[sku] = self.on_hand.get(sku, 0) + qty
        self.trail.append(("add", sku, qty))

    def available(self, sku):
        return max(0, self.on_hand.get(sku, 0) - self.reserved.get(sku, 0))

    def reserve(self, sku, qty):
        if sku not in self.on_hand:
            raise KeyError(sku)
        if qty < 1:
            raise StockError("qty must be >= 1")
        if qty > self.available(sku):
            raise StockError("insufficient")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))

    def release(self, sku, qty):
        if sku not in self.on_hand:
            raise KeyError(sku)
        current = self.reserved.get(sku, 0)
        self.reserved[sku] = max(0, current - qty)
        self.trail.append(("release", sku, qty))
`
		p, tot, f := gradeInventory(map[string]string{"inventory.py": src})
		t.Logf("得分 %d/%d  失败=%v", p, tot, f)
		if p != tot {
			t.Errorf("正确解应满分")
		}
	})

	t.Run("未修复的原始文件仍应低分", func(t *testing.T) {
		p, tot, _ := gradeDebug(map[string]string{"stats.py": debugOriginalStats})
		t.Logf("原始文件 %d/%d", p, tot)
		if p == tot {
			t.Error("未修复的文件不应满分")
		}
	})
}
