package web

import "testing"

func TestGradeShiftAPKExpectedOutput(t *testing.T) {
	files := map[string]string{"schedule.json": `{"Mon":"Dan","Tue":"Ben","Wed":"Cara","Thu":"Ann"}`}
	passed, total, failures := gradeShift(files)
	if passed != total || total != 10 || len(failures) != 0 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeSalesAPKExpectedOutput(t *testing.T) {
	files := map[string]string{"report.json": `{
		"revenueByRegion":{"north":80,"south":80,"east":70},
		"topMonth":"2026-02","totalRevenue":230,"topRegion":"south"
	}`}
	passed, total, failures := gradeSales(files)
	if passed != total || total != 6 || len(failures) != 0 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeSalesAllowsSnakeCaseAndTieButRejectsWrongRegion(t *testing.T) {
	files := map[string]string{"report.json": `{
		"revenue_by_region":{"north":80,"south":80,"east":70},
		"top_month":"2026-02","total_revenue":230,"top_region":"west"
	}`}
	passed, total, failures := gradeSales(files)
	if passed != 5 || total != 6 || len(failures) != 1 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeLedgerAPKExpectedOutput(t *testing.T) {
	files := map[string]string{"state.json": `{
		"balances":{"A":0,"B":115,"C":30},"rejected":4,"applied":6
	}`}
	passed, total, failures := gradeLedger(files)
	if passed != total || total != 6 || len(failures) != 0 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeRouteAPKExpectedOutput(t *testing.T) {
	files := map[string]string{"route.json": `{"path":["A","C","B","D","E","F"],"cost":13}`}
	passed, total, failures := gradeRoute(files)
	if passed != total || total != 3 || len(failures) != 0 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeRouteRejectsMalformedArtifact(t *testing.T) {
	passed, total, failures := gradeRoute(map[string]string{"route.json": `not-json`})
	if passed != 0 || total != 3 || len(failures) != 1 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func inventoryFixedSource() string {
	return `class StockError(Exception):
    pass

class Inventory:
    """Warehouse stock. CONTRACT: preserve this behavior."""
    def __init__(self):
        self.on_hand = {}
        self.reserved = {}
        self.trail = []
    def add(self, sku, qty):
        if not isinstance(qty, int) or qty < 1:
            raise ValueError("qty")
        self.on_hand[sku] = self.on_hand.get(sku, 0) + qty
    def reserve(self, sku, qty):
        if sku not in self.on_hand:
            raise KeyError(sku)
        if qty <= 0:
            raise ValueError("qty")
        available = self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)
        if qty > available:
            raise StockError("insufficient")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        self.trail.append(("reserve", sku, qty))
    def release(self, sku, qty):
        result = self.reserved.get(sku, 0) - qty
        if result < 0:
            result = 0
        self.reserved[sku] = result
    def available(self, sku):
        return max(0, self.on_hand.get(sku, 0) - self.reserved.get(sku, 0))
`
}

func TestGradeInventoryAPKExpectedOutput(t *testing.T) {
	passed, total, failures := gradeInventory(map[string]string{"inventory.py": inventoryFixedSource()})
	if passed != total || total != 7 || len(failures) != 0 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeInventoryDetectsOriginalDefects(t *testing.T) {
	source := `class StockError(Exception): pass
class Inventory:
    """CONTRACT"""
    def add(self, sku, qty):
        if qty < 0: raise ValueError()
    def reserve(self, sku, qty):
        self.trail.append(("reserve", sku, qty))
        if qty > self.on_hand.get(sku, 0): raise StockError()
    def release(self, sku, qty):
        self.reserved[sku] = self.reserved.get(sku, 0) - qty
    def available(self, sku):
        return self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)
`
	passed, total, failures := gradeInventory(map[string]string{"inventory.py": source})
	if passed != 2 || total != 7 || len(failures) != 5 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}

func TestGradeInventoryMissingFile(t *testing.T) {
	passed, total, failures := gradeInventory(map[string]string{})
	if passed != 0 || total != 7 || len(failures) != 1 {
		t.Fatalf("passed=%d total=%d failures=%v", passed, total, failures)
	}
}
