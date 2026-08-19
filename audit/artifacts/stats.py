def summarize(rows):
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
