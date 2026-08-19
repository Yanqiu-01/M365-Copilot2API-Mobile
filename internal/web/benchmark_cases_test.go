package web

import (
	"strings"
	"testing"
)

func TestGradeInventory(t *testing.T) {
	// Test case 1: missing file
	passed, total, failures := gradeInventory(map[string]string{})
	if passed != 0 || total != 7 || len(failures) != 1 {
		t.Errorf("missing file: got (%d/%d, %d failures), want (0/7, 1 failure)", passed, total, len(failures))
	}

	// Test case 2: minimal valid inventory
	valid := `
class Inventory:
    """CONTRACT: preserve all rules"""
    def add(self, sku, qty):
        if qty < 1:
            raise ValueError("qty must be positive")
        self.on_hand[sku] = self.on_hand.get(sku, 0) + qty
    
    def reserve(self, sku, qty):
        if sku not in self.on_hand:
            raise KeyError(f"unknown sku {sku}")
        available = self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)
        if qty > available:
            raise StockError("insufficient stock")
        self.trail.append(("reserve", sku, qty))
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
    
    def release(self, sku, qty):
        self.reserved[sku] = max(0, self.reserved.get(sku, 0) - qty)
    
    def available(self, sku):
        return self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)

class StockError(Exception):
    pass
`
	passed, total, failures = gradeInventory(map[string]string{"inventory.py": valid})
	if passed != 7 || total != 7 {
		t.Errorf("valid inventory: got %d/%d (failures: %v), want 7/7", passed, total, failures)
	}

	// Test case 3: defect 1 - qty < 0 instead of qty < 1
	defect1 := strings.Replace(valid, "if qty < 1:", "if qty < 0:", 1)
	passed, total, failures = gradeInventory(map[string]string{"inventory.py": defect1})
	if passed != 6 || total != 7 {
		t.Errorf("defect1 (qty<0): got %d/%d, want 6/7", passed, total)
	}

	// Test case 4: defect 2 - trail written before validation
	defect2 := `
class Inventory:
    """CONTRACT"""
    def add(self, sku, qty):
        if qty < 1:
            raise ValueError("qty must be positive")
        self.on_hand[sku] = self.on_hand.get(sku, 0) + qty
    def reserve(self, sku, qty):
        self.trail.append(("reserve", sku, qty))
        if sku not in self.on_hand:
            raise KeyError("unknown")
        raise StockError("bad")
    def release(self, sku, qty):
        self.reserved[sku] = max(0, self.reserved.get(sku, 0) - qty)
    def available(self, sku):
        return self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)
class StockError(Exception):
    pass
`
	passed, total, _ = gradeInventory(map[string]string{"inventory.py": defect2})
	if passed != 6 || total != 7 {
		t.Errorf("defect2 (trail before validation): got %d/%d, want 6/7", passed, total)
	}

	// Test case 5: defect 3 - missing KeyError
	defect3 := strings.Replace(valid, "raise KeyError", "raise StockError", 1)
	passed, total, _ = gradeInventory(map[string]string{"inventory.py": defect3})
	if passed != 6 || total != 7 {
		t.Errorf("defect3 (no KeyError): got %d/%d, want 6/7", passed, total)
	}

	// Test case 6: defect 4 - comparing on_hand instead of available
	defect4 := strings.Replace(valid, "available = self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)", "# removed", 1)
	defect4 = strings.Replace(defect4, "if qty > available:", "if qty > self.on_hand.get(sku, 0):", 1)
	passed, total, _ = gradeInventory(map[string]string{"inventory.py": defect4})
	if passed != 6 || total != 7 {
		t.Errorf("defect4 (on_hand instead of available): got %d/%d, want 6/7", passed, total)
	}

	// Test case 7: defect 5 - release allows negative reserved
	defect5 := strings.Replace(valid, "self.reserved[sku] = max(0, self.reserved.get(sku, 0) - qty)", "self.reserved[sku] = self.reserved.get(sku, 0) - qty", 1)
	passed, total, _ = gradeInventory(map[string]string{"inventory.py": defect5})
	if passed != 6 || total != 7 {
		t.Errorf("defect5 (negative reserved): got %d/%d, want 6/7", passed, total)
	}
}

func TestGradeInventoryContract(t *testing.T) {
	// Missing CONTRACT marker
	noContract := `
class Inventory:
    """Some docstring"""
    def add(self, sku, qty):
        if qty < 1:
            raise ValueError("positive only")
        self.on_hand[sku] = self.on_hand.get(sku, 0) + qty
    def reserve(self, sku, qty):
        if sku not in self.on_hand:
            raise KeyError("unknown")
        available = self.available(sku)
        if qty > available:
            raise StockError("insufficient")
        self.trail.append(("reserve", sku, qty))
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
    def release(self, sku, qty):
        self.reserved[sku] = max(0, self.reserved.get(sku, 0) - qty)
    def available(self, sku):
        return self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)
class StockError(Exception):
    pass
`
	passed, total, failures := gradeInventory(map[string]string{"inventory.py": noContract})
	if passed != 6 || total != 7 {
		t.Errorf("missing CONTRACT: got %d/%d, want 6/7 (failures: %v)", passed, total, failures)
	}
	if !strings.Contains(strings.Join(failures, " "), "CONTRACT") {
		t.Errorf("expected CONTRACT in failure message, got: %v", failures)
	}
}

func TestFunctionBody(t *testing.T) {
	source := `
class Inventory:
    def add(self, sku, qty):
        if qty < 1:
            raise ValueError("positive")
        self.on_hand[sku] = qty
    
    def reserve(self, sku, qty):
        # validation
        if sku not in self.on_hand:
            raise KeyError("unknown")
        self.trail.append(("reserve", sku, qty))
`
	addBody := functionBody(source, "def add")
	if !strings.Contains(addBody, "if qty < 1") || !strings.Contains(addBody, "self.on_hand[sku] = qty") {
		t.Errorf("add body extraction failed: %q", addBody)
	}
	if strings.Contains(addBody, "def reserve") {
		t.Errorf("add body should not include reserve: %q", addBody)
	}

	reserveBody := functionBody(source, "def reserve")
	if !strings.Contains(reserveBody, "KeyError") || !strings.Contains(reserveBody, "self.trail.append") {
		t.Errorf("reserve body extraction failed: %q", reserveBody)
	}
}
