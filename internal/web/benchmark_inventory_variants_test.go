package web

import (
	"strings"
	"testing"
)

// 逐项体检：把 7 条判定各自的常见正确写法都试一遍，找出还有盲区的项。
func TestInventoryAllChecksAcceptVariants(t *testing.T) {
	// 一份全部修对的基准，然后逐项替换写法。
	build := func(parts map[string]string) string {
		get := func(k, def string) string {
			if v, ok := parts[k]; ok {
				return v
			}
			return def
		}
		return `class StockError(Exception):
    pass


class Inventory:
    """Warehouse stock.

    CONTRACT (this implementation violates five of these):
      - add(sku, qty): qty must be >= 1 else ValueError.
    """

    def __init__(self):
        self.on_hand = {}
        self.reserved = {}
        self.trail = []

    def add(self, sku, qty):
` + get("add", `        if qty < 1:
            raise ValueError("qty must be >= 1")`) + `
        self.on_hand[sku] = self.on_hand.get(sku, 0) + qty
        self.trail.append(("add", sku, qty))
        return self.on_hand[sku]

    def reserve(self, sku, qty):
` + get("reserve", `        if sku not in self.on_hand:
            raise KeyError(sku)
        if qty < 1:
            raise ValueError("qty must be >= 1")
        if qty > self.available(sku):
            raise StockError("not enough stock")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))
        return self.reserved[sku]`) + `

    def release(self, sku, qty):
` + get("release", `        current = self.reserved.get(sku, 0)
        self.reserved[sku] = max(0, current - qty)
        self.trail.append(("release", sku, qty))
        return self.reserved[sku]`) + `

    def available(self, sku):
` + get("available", `        return max(0, self.on_hand.get(sku, 0) - self.reserved.get(sku, 0))`) + `
`
	}

	check := func(t *testing.T, label string, parts map[string]string) {
		src := build(parts)
		p, tot, f := gradeInventory(map[string]string{"inventory.py": src})
		if p != tot {
			t.Errorf("[%s] %d/%d 失败=%v", label, p, tot, f)
		}
	}

	t.Run("基准全对", func(t *testing.T) { check(t, "baseline", nil) })

	// 缺陷3：未知 sku 抛 KeyError 的多种写法
	for name, body := range map[string]string{
		"sku not in on_hand": `        if sku not in self.on_hand:
            raise KeyError(sku)
        if qty < 1:
            raise ValueError("bad qty")
        if qty > self.available(sku):
            raise StockError("no stock")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))`,
		"直接索引触发 KeyError": `        on_hand = self.on_hand[sku]
        if qty < 1:
            raise ValueError("bad qty")
        if qty > on_hand - self.reserved.get(sku, 0):
            raise StockError("no stock")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))`,
		"try/except 转抛": `        try:
            stock = self.on_hand[sku]
        except KeyError:
            raise KeyError(sku)
        if qty < 1:
            raise ValueError("bad qty")
        if qty > stock - self.reserved.get(sku, 0):
            raise StockError("no stock")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))`,
	} {
		t.Run("缺陷3/"+name, func(t *testing.T) { check(t, name, map[string]string{"reserve": body}) })
	}

	// 缺陷4：依据可用量而非 on_hand
	for name, body := range map[string]string{
		"调用 available()": `        if sku not in self.on_hand:
            raise KeyError(sku)
        if qty < 1:
            raise ValueError("bad")
        if qty > self.available(sku):
            raise StockError("no stock")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))`,
		"内联计算可用量": `        if sku not in self.on_hand:
            raise KeyError(sku)
        if qty < 1:
            raise ValueError("bad")
        free = self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)
        if qty > free:
            raise StockError("no stock")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))`,
		"先算 remaining 变量": `        if sku not in self.on_hand:
            raise KeyError(sku)
        if qty < 1:
            raise ValueError("bad")
        remaining = self.on_hand[sku] - self.reserved.get(sku, 0)
        if remaining < qty:
            raise StockError("no stock")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))`,
	} {
		t.Run("缺陷4/"+name, func(t *testing.T) { check(t, name, map[string]string{"reserve": body}) })
	}

	// 缺陷2：失败不写 trail —— 校验必须在 append 之前
	t.Run("缺陷2/append 在末尾", func(t *testing.T) {
		check(t, "append last", map[string]string{"reserve": `        if sku not in self.on_hand:
            raise KeyError(sku)
        if qty < 1:
            raise ValueError("bad")
        if qty > self.available(sku):
            raise StockError("no stock")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))
        return self.reserved[sku]`})
	})

	// CONTRACT 保留：模型可能重排 docstring
	t.Run("CONTRACT 仍在", func(t *testing.T) {
		src := build(nil)
		if !strings.Contains(src, "CONTRACT") {
			t.Skip()
		}
		check(t, "contract", nil)
	})
}
