#!/usr/bin/env python3
import json, os, collections

HERE = os.path.dirname(os.path.abspath(__file__))
FD = os.path.join(HERE, "findings")

# ---- status mapping (id -> status) ----
FIXED = set()
NONDEF = set()
RECORDED = set()

def add(area, ids, bucket):
    for i in ids:
        bucket.add(f"{area}-{i:03d}")

add("backend-core", [2,3,4,5], RECORDED)
add("backend-core", [1], RECORDED)  # P2 ask_user window: recorded (delicate state machine)
add("backend-http", [1,2,3,4,6,9], FIXED)
add("backend-http", [5], NONDEF)
add("backend-http", [7,8,10,11], RECORDED)
add("backend-integrations", [1,2,10], FIXED)
add("backend-integrations", [3,4,5,6,7,8,9,11,12,13,14], RECORDED)
add("backend-persona", [1,2,3,5,6,7,8,9,10,12], FIXED)
add("backend-persona", [4,11], RECORDED)
add("test-failures", [1,2,3], FIXED)
add("render-validation", [1,2,4], FIXED)
add("render-validation", [3], RECORDED)
add("ui-deep", [1,2,3,4,5,6,7], FIXED)
add("security", [1,2], FIXED)
add("security", [3,4,5,6,7,8], RECORDED)
add("frontend-core", [1,2,3,4,5,6], FIXED)
add("frontend-core", [7], RECORDED)
add("frontend-viewers", [1,2,3,4,5,6,8,11], FIXED)
add("frontend-viewers", [7,9,10,12,13,14,15,16], RECORDED)
add("frontend-css", [1,2,3,5,6,7,9,10,13,14], FIXED)
add("frontend-css", [4,8,11,12], RECORDED)
add("runtime-functional", [1,2,3,5], FIXED)
add("runtime-functional", [4], RECORDED)

# ---- duplicate / cross-area clusters (canonical first) ----
CLUSTERS = {
    "dup-mermaid": ["frontend-viewers-004", "frontend-core-002", "security-001"],
    "dup-sqlite-xss": ["frontend-viewers-002", "security-002"],
    "dup-csp-inline": ["runtime-functional-002", "render-validation-003"],
    "dup-highlight-404": ["runtime-functional-003"],
}
DUP_OF = {}
for canon, members in CLUSTERS.items():
    for m in members[1:]:
        DUP_OF[m] = canon

files = sorted(f for f in os.listdir(FD) if f.endswith(".json"))
records = []
for fn in files:
    d = json.load(open(os.path.join(FD, fn), encoding="utf-8"))
    area = fn[:-5]
    for f in d.get("findings", []):
        fid = f["id"]
        if fid in FIXED: status = "fixed"
        elif fid in NONDEF: status = "non-defect"
        elif fid in RECORDED: status = "recorded"
        else: status = "??"
        rec = {
            "id": fid, "area": area,
            "severity": f.get("severity", ""),
            "category": f.get("category", ""),
            "file": f.get("file", ""),
            "line": str(f.get("line", "")),
            "confidence": f.get("confidence", ""),
            "status": status,
            "needs_user_decision": bool(f.get("needs_user_decision", False)),
            "title": f.get("title", ""),
            "dup_of": DUP_OF.get(fid, ""),
        }
        records.append(rec)

SEV = ["P0", "P1", "P2", "P3"]
def counts(rs):
    c = collections.Counter(r["severity"] for r in rs)
    return {s: c.get(s, 0) for s in SEV}

unique = [r for r in records if not r["dup_of"]]
stat = {
    "total_records": len(records),
    "unique_records": len(unique),
    "raw_counts": counts(records),
    "unique_counts": counts(unique),
    "status_counts": collections.Counter(r["status"] for r in records),
    "unique_status_counts": collections.Counter(r["status"] for r in unique),
}

json.dump({"stats": {k: dict(v) if isinstance(v, collections.Counter) else v
                     for k, v in stat.items()},
           "records": records},
          open(os.path.join(HERE, "master-findings-full.json"), "w", encoding="utf-8"),
          ensure_ascii=False, indent=2)

# ---- markdown table (all records) ----
order = {"P0":0,"P1":1,"P2":2,"P3":3}
records.sort(key=lambda r: (order.get(r["severity"],9), r["id"]))
ZH = {"fixed":"✅已修","non-defect":"➖非缺陷","recorded":"📝记录","??":"?"}
lines = ["| ID | 级 | 分类 | 位置 | 置信 | 状态 | 标题 |",
         "|----|----|------|------|------|------|------|"]
for r in records:
    loc = f"{r['file']}:{r['line']}"
    tag = ZH[r["status"]]
    if r["dup_of"]: tag += f" (重复→{r['dup_of']})"
    if r["needs_user_decision"]: tag += " ⚠️决策"
    title = r["title"].replace("|","/")
    lines.append(f"| {r['id']} | {r['severity']} | {r['category']} | `{loc}` | {r['confidence']} | {tag} | {title} |")
open(os.path.join(HERE, "master-findings-table.md"), "w", encoding="utf-8").write("\n".join(lines))

print("total", stat["total_records"], "unique", stat["unique_records"])
print("raw", stat["raw_counts"])
print("unique", stat["unique_counts"])
print("status", dict(stat["status_counts"]))
print("unique_status", dict(stat["unique_status_counts"]))
unknown = [r["id"] for r in records if r["status"]=="??"]
print("UNMAPPED", unknown)
