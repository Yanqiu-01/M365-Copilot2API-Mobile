package web

// 本文件的常量是评测任务的初始产物，逐字节取自 APK 的 rodata。
// APK benchTasks 的调用图不含 embed 相关符号，产物以 Go 字符串常量形式
// 编译进 rodata，故此处同样使用常量而非 embed 资源。
//
// 提取地址与长度（均已逐字节校验，见 audit/artifacts/）：
//   inventory.py    0x522000+469   1724
//   stats.py        0x51f000+722    531
//   run_report.txt  0x51e000+3498   387
//   users.py        0x51f000+1789   544
//   staff.py        0x51f000+2333   552
//   people.json     0x51e000+909    203
//   ledger.txt      0x51d000+2165   144
//   sales.csv       0x51e000+2854   320

// benchInventorySource 对应 inventory.py（1724 字节）。
const benchInventorySource = `class StockError(Exception):
    pass


class Inventory:
    """Warehouse stock with reservations and an audit trail.

    CONTRACT (this implementation violates five of these):
      - add(sku, qty): qty must be >= 1 else ValueError.
        Adding an existing sku increases its quantity.
      - reserve(sku, qty): qty must be >= 1 else ValueError.
        Raises KeyError for an unknown sku.
        Raises StockError if qty exceeds the AVAILABLE quantity
        (available = on_hand - reserved). Reserved stock is never double-booked.
      - release(sku, qty): cancels a reservation; reserved must never go below 0.
      - available(sku): returns on_hand - reserved, never negative.
      - trail records one entry per SUCCESSFUL mutation only, in order.
    """

    def __init__(self):
        self.on_hand = {}
        self.reserved = {}
        self.trail = []

    def add(self, sku, qty):
        if qty < 0:
            raise ValueError("qty must be >= 1")
        self.on_hand[sku] = self.on_hand.get(sku, 0) + qty
        self.trail.append(("add", sku, qty))
        return self.on_hand[sku]

    def reserve(self, sku, qty):
        if qty < 1:
            raise ValueError("qty must be >= 1")
        self.trail.append(("reserve", sku, qty))
        if qty > self.on_hand.get(sku, 0):
            raise StockError("not enough stock")
        self.reserved[sku] = self.reserved.get(sku, 0) + qty
        return self.reserved[sku]

    def release(self, sku, qty):
        self.reserved[sku] = self.reserved.get(sku, 0) - qty
        self.trail.append(("release", sku, qty))
        return self.reserved[sku]

    def available(self, sku):
        return self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)
`

// benchStatsSource 对应 stats.py（531 字节）。
const benchStatsSource = `def summarize(rows):
    """Return {"count", "total", "mean", "max"} for a list of numbers.

    An empty list must return count 0, total 0, mean None, max None.
    """
    total = 0
    for value in rows:
        total += value
    count = len(rows)
    mean = total / count
    return {
        "count": count,
        "total": total,
        "mean": mean,
        "max": max(rows),
    }


def format_report(rows):
    stats = summarize(rows)
    return f"count={stats['count']} total={stats['total']} mean={stats['mean'].2f}"
`

// benchRunReport 对应 run_report.txt（387 字节）。
const benchRunReport = `$ python3 -c "import stats; print(stats.format_report([1,2,3]))"
  File "stats.py", line 20
    return f"count={stats['count']} total={stats['total']} mean={stats['mean'].2f}"
                                                                              ^
SyntaxError: f-string: invalid syntax

$ python3 -c "import stats; print(stats.summarize([]))"
ZeroDivisionError: division by zero
`

// benchUsersSource 对应 users.py（544 字节）。
const benchUsersSource = `import json


def load_users(path):
    with open(path, encoding="utf-8") as f:
        raw = json.load(f)
    out = []
    for item in raw:
        if not isinstance(item, dict):
            continue
        name = item.get("name")
        if not name or not isinstance(name, str):
            continue
        age = item.get("age")
        if not isinstance(age, int) or age < 0 or age > 150:
            continue
        out.append({"name": name.strip().title(), "age": age})
    out.sort(key=lambda d: (d["age"], d["name"]))
    return out
`

// benchStaffSource 对应 staff.py（552 字节）。
const benchStaffSource = `import json


def load_staff(path):
    with open(path, encoding="utf-8") as f:
        raw = json.load(f)
    result = []
    for entry in raw:
        if not isinstance(entry, dict):
            continue
        nm = entry.get("name")
        if not nm or not isinstance(nm, str):
            continue
        yrs = entry.get("age")
        if not isinstance(yrs, int) or yrs < 0 or yrs > 150:
            continue
        result.append({"name": nm.strip().title(), "age": yrs})
    result.sort(key=lambda d: (d["age"], d["name"]))
    return result
`

// benchPeopleJSON 对应 people.json（203 字节）。
const benchPeopleJSON = `[{"name":"  alice ","age":30},{"name":"BOB","age":25},{"name":"carol","age":25},{"name":"","age":40},{"name":"dave","age":-1},{"name":"erin","age":200},{"name":"frank","age":"x"},"not-a-dict",{"age":22}]`

// benchLedgerText 对应 ledger.txt（144 字节）。
const benchLedgerText = `DEPOSIT A 100
DEPOSIT B 50
WITHDRAW A 30
WITHDRAW B 80
TRANSFER A B 40
TRANSFER B C 200
DEPOSIT C -10
TRANSFER A C 30
WITHDRAW C 0
DEPOSIT B 25
`

// benchSalesCSV 对应 sales.csv（320 字节）。
const benchSalesCSV = `date,region,product,units,unit_price
2026-01-05,north,widget,10,2.50
2026-01-06,south,widget,4,2.50
2026-01-07,north,gadget,3,10.00
2026-02-01,north,widget,6,2.50
2026-02-02,south,gadget,5,10.00
2026-02-03,east,widget,20,2.50
2026-03-01,south,widget,8,2.50
2026-03-02,north,gadget,1,10.00
2026-03-03,east,gadget,2,10.00
`
