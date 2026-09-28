#!/usr/bin/env python3
"""Merge per-agent findings JSON into a master list and surface candidate duplicates.
Read-only with respect to source; writes master-findings.json + duplicate-candidates.txt."""
import json, glob, os, re, sys

BASE = os.path.dirname(os.path.abspath(__file__))
FDIR = os.path.join(BASE, "findings")

def norm_file(f):
    return (f or "").strip().lstrip("./")

def line_start(l):
    m = re.match(r"\s*(\d+)", str(l or ""))
    return int(m.group(1)) if m else -1

def title_key(t):
    t = (t or "").lower()
    t = re.sub(r"[`'\"“”‘’\(\)\[\]:：,，.。、/\\_\-\s]+", "", t)
    return t

records = []
for path in sorted(glob.glob(os.path.join(FDIR, "*.json"))):
    slug = os.path.splitext(os.path.basename(path))[0]
    try:
        d = json.load(open(path))
    except Exception as e:
        print("SKIP invalid", path, e); continue
    for x in d.get("findings", []):
        x["_source"] = slug
        x["_file"] = norm_file(x.get("file"))
        x["_line"] = line_start(x.get("line"))
        x["_tkey"] = title_key(x.get("title"))
        records.append(x)

# cluster candidates: same file AND (line within 12 OR title jaccard-ish overlap)
parent = list(range(len(records)))
def find(a):
    while parent[a] != a:
        parent[a] = parent[parent[a]]; a = parent[a]
    return a
def union(a,b):
    parent[find(a)] = find(b)

def tshare(a,b):
    x,y = records[a]["_tkey"], records[b]["_tkey"]
    if not x or not y: return False
    # containment of a meaningful substring
    if len(x) >= 8 and (x in y or y in x): return True
    sx = set(re.findall(r".{2}", x)); sy = set(re.findall(r".{2}", y))
    if sx and sy:
        j = len(sx&sy)/len(sx|sy)
        return j >= 0.6
    return False

for i in range(len(records)):
    for j in range(i+1, len(records)):
        ri, rj = records[i], records[j]
        same_file = ri["_file"] and ri["_file"] == rj["_file"]
        close_line = ri["_line"] >= 0 and rj["_line"] >= 0 and abs(ri["_line"]-rj["_line"]) <= 12
        if same_file and (close_line or tshare(i,j)):
            union(i,j)
        elif not same_file and tshare(i,j) and ri.get("category")==rj.get("category"):
            union(i,j)

groups = {}
for i in range(len(records)):
    groups.setdefault(find(i), []).append(i)

dup_groups = [g for g in groups.values() if len(g) > 1]
dup_groups.sort(key=lambda g: (records[g[0]]["_file"], records[g[0]]["_line"]))

with open(os.path.join(BASE, "duplicate-candidates.txt"), "w") as fh:
    for g in dup_groups:
        fh.write("=== candidate cluster ===\n")
        for i in g:
            r = records[i]
            fh.write(f"  [{r['_source']}] {r.get('severity')} {r.get('category')} | {r['_file']}:{r.get('line')} | {r.get('title')}\n")
        fh.write("\n")

# severity tally
sev = {}
for r in records:
    sev[r.get("severity","?")] = sev.get(r.get("severity","?"),0)+1

# write a raw merged list (stable ids assigned later after dedup adjudication)
master = [{k:v for k,v in r.items() if not k.startswith("_") or k in ("_source",)} for r in records]
json.dump({"total": len(records), "severity": sev,
           "dup_clusters": len(dup_groups),
           "records": records}, open(os.path.join(BASE,"master-findings.json"),"w"),
          ensure_ascii=False, indent=2)

print("total records:", len(records))
print("severity:", dict(sorted(sev.items())))
print("duplicate candidate clusters:", len(dup_groups))
byfile = {}
for r in records:
    byfile[r["_source"]] = byfile.get(r["_source"],0)+1
print("per source:", byfile)
