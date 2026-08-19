import json


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
