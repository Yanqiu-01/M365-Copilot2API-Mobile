package web

import "testing"

// 模型修对缺陷1和缺陷5的多种合理写法，判定必须都认。
func TestDefect1And5AcceptCommonFixes(t *testing.T) {
	base := func(addGuard, releaseBody string) string {
		return `class StockError(Exception):
    pass


class Inventory:
    """CONTRACT (this implementation violates five of these):
      - add(sku, qty): qty must be >= 1 else ValueError.
    """

    def __init__(self):
        self.on_hand = {}
        self.reserved = {}
        self.trail = []

    def add(self, sku, qty):
` + addGuard + `
        self.on_hand[sku] = self.on_hand.get(sku, 0) + qty
        self.trail.append(("add", sku, qty))
        return self.on_hand[sku]

    def reserve(self, sku, qty):
        if sku not in self.on_hand:
            raise KeyError(sku)
        if qty < 1:
            raise ValueError("qty must be >= 1")
        if qty > self.available(sku):
            raise StockError("not enough stock")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))
        return self.reserved[sku]

    def release(self, sku, qty):
` + releaseBody + `

    def available(self, sku):
        return max(0, self.on_hand.get(sku, 0) - self.reserved.get(sku, 0))
`
	}

	goodRelease := `        current = self.reserved.get(sku, 0)
        self.reserved[sku] = max(0, current - qty)
        self.trail.append(("release", sku, qty))
        return self.reserved[sku]`

	t.Run("缺陷1 各种写法", func(t *testing.T) {
		for name, guard := range map[string]string{
			"qty < 1":       `        if qty < 1:` + "\n" + `            raise ValueError("qty must be >= 1")`,
			"qty <= 0":      `        if qty <= 0:` + "\n" + `            raise ValueError("qty must be >= 1")`,
			"not qty >= 1":  `        if not qty >= 1:` + "\n" + `            raise ValueError("qty must be >= 1")`,
			"qty < 1 单行":    `        if qty < 1: raise ValueError("qty must be >= 1")`,
			"isinstance 检查": `        if not isinstance(qty, int) or qty < 1:` + "\n" + `            raise ValueError("qty must be >= 1")`,
			"用 1 > qty":     `        if 1 > qty:` + "\n" + `            raise ValueError("qty must be >= 1")`,
			"先判类型再判范围":      `        if not isinstance(qty, int):` + "\n" + `            raise ValueError("qty must be int")` + "\n" + `        if qty <= 0:` + "\n" + `            raise ValueError("qty must be >= 1")`,
		} {
			src := base(guard, goodRelease)
			p, tot, f := gradeInventory(map[string]string{"inventory.py": src})
			hasD1 := false
			for _, x := range f {
				if x == "缺陷1 add 拒绝 qty=0" {
					hasD1 = true
				}
			}
			if hasD1 {
				t.Errorf("[%s] 缺陷1 被误判未修复（%d/%d）失败=%v", name, p, tot, f)
			}
		}
	})

	t.Run("缺陷5 各种写法", func(t *testing.T) {
		guard := `        if qty < 1:` + "\n" + `            raise ValueError("qty must be >= 1")`
		for name, rel := range map[string]string{
			"max(0, ...)": goodRelease,
			"min 限制释放量": `        current = self.reserved.get(sku, 0)
        actual = min(qty, current)
        self.reserved[sku] = current - actual
        self.trail.append(("release", sku, actual))
        return self.reserved[sku]`,
			"先比较再减": `        current = self.reserved.get(sku, 0)
        if qty > current:
            qty = current
        self.reserved[sku] = current - qty
        self.trail.append(("release", sku, qty))
        return self.reserved[sku]`,
			"减后夹紧": `        self.reserved[sku] = self.reserved.get(sku, 0) - qty
        if self.reserved[sku] < 0:
            self.reserved[sku] = 0
        self.trail.append(("release", sku, qty))
        return self.reserved[sku]`,
			"用 max 内联": `        self.reserved[sku] = max(self.reserved.get(sku, 0) - qty, 0)
        self.trail.append(("release", sku, qty))
        return self.reserved[sku]`,
		} {
			src := base(guard, rel)
			p, tot, f := gradeInventory(map[string]string{"inventory.py": src})
			hasD5 := false
			for _, x := range f {
				if x == "缺陷5 release 后 reserved 不为负" {
					hasD5 = true
				}
			}
			if hasD5 {
				t.Errorf("[%s] 缺陷5 被误判未修复（%d/%d）失败=%v", name, p, tot, f)
			}
		}
	})
}
