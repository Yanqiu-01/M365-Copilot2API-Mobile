import json


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
