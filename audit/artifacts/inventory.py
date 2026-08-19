class StockError(Exception):
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
