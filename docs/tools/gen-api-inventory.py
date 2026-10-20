#!/usr/bin/env python3
"""Generate docs/api-inventory.md from routers/router.go (+ shop/router.go).

Usage:  python3 docs/tools/gen-api-inventory.py
"""

import datetime
import json
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / "docs" / "api-inventory.md"
SCRATCH = pathlib.Path("/tmp")


def extract(path: pathlib.Path, base_prefix: str):
    """Collect METHOD/path pairs, resolving `x := parent.Group("prefix")` chains."""
    prefixes = {"v4": base_prefix}
    routes = []
    group_assign = re.compile(r'^\s*(\w+)\s*:?=\s*(\w+)\.Group\(\s*"([^"]*)"')
    route_re = re.compile(r'^\s*(\w+)\.(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\("([^"]*)"')
    handle_re = re.compile(r"controllers\.([A-Za-z_][\w.]*)")
    lines = path.read_text(encoding="utf-8").split("\n")
    for idx, line in enumerate(lines):
        code = line.split("//")[0]
        m = group_assign.match(code)
        if m:
            var, parent, name = m.group(1), m.group(2), m.group(3)
            if parent in prefixes:
                prefixes[var] = (prefixes[parent].rstrip("/") + "/" + name.strip("/")) if name else prefixes[parent]
            continue
        m = route_re.match(code)
        if m:
            var, method, sub = m.group(1), m.group(2), m.group(3)
            if var not in prefixes:
                continue
            full = (prefixes[var].rstrip("/") + "/" + sub.strip("/")) if sub else prefixes[var]
            full = "/" + full.lstrip("/")
            handler = ""
            for look in range(idx, min(idx + 8, len(lines))):
                for h in handle_re.finditer(lines[look]):
                    name = h.group(1)
                    # skip the generic binding wrappers to show the real controller
                    if name in ("FromJSON", "FromUri", "FromQuery"):
                        continue
                    handler = name
                    break
                if handler:
                    break
            routes.append({"method": method, "path": full, "handler": handler})
    return routes


def classify(path: str) -> str:
    p = path[len("/api/v4") :] if path.startswith("/api/v4") else path
    if p.startswith("/admin"):
        return "管理员"
    if p.startswith("/shop"):
        return "小店"
    if p.startswith(("/slave", "/callback", "/payment/notify", "/upload")):
        return "从机 / 回调"
    if p.startswith(("/session", "/site", "/s/", "/vas/payment/status", "/share/:id")):
        return "公开"
    return "用户（登录）"


def norm(path: str) -> str:
    return re.sub(r":[A-Za-z_]\w*", ":p", path)


def main() -> int:
    routes = extract(ROOT / "routers" / "router.go", "/api/v4")
    shop_routes = extract(ROOT / "shop" / "router.go", "/api/v4/shop")
    # shop/router.go declares `group := v4.Group("shop")`, the extractor already
    # prefixes /api/v4/shop, so filter out the duplicated group line results.
    routes = routes + [r for r in shop_routes if not r["path"].startswith("/api/v4/shop/api")]

    seen = set()
    unique = []
    for r in routes:
        key = (r["method"], norm(r["path"]))
        if key in seen:
            continue
        seen.add(key)
        unique.append(r)

    buckets = {}
    for r in unique:
        buckets.setdefault(classify(r["path"]), []).append(r)

    order = ["公开", "用户（登录）", "小店", "管理员", "从机 / 回调"]
    methods = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"]

    lines = []
    lines.append("# API 接口清单")
    lines.append("")
    lines.append("> 自动生成，请勿手工编辑。重新生成：`python3 docs/tools/gen-api-inventory.py`。")
    lines.append("")
    lines.append(f"- 来源：`routers/router.go` + `shop/router.go`")
    lines.append(f"- 生成时间：{datetime.datetime.now().strftime('%Y-%m-%d %H:%M')}")
    lines.append(f"- 接口总数：**{len(unique)}**（去重后 `METHOD + 路径`，路径参数统一记为 `:param`）")
    lines.append("")
    lines.append("## 汇总")
    lines.append("")
    lines.append("| 分类 | 接口数 | " + " | ".join(methods) + " |")
    lines.append("| --- | --- | " + " | ".join("---" for _ in methods) + " |")
    for name in order:
        items = buckets.get(name, [])
        if not items:
            continue
        counts = [str(sum(1 for r in items if r["method"] == m)) for m in methods]
        lines.append(f"| {name} | {len(items)} | " + " | ".join(counts) + " |")
    lines.append(f"| **合计** | **{len(unique)}** | " + " | ".join(
        str(sum(1 for r in unique if r["method"] == m)) for m in methods) + " |")
    lines.append("")

    for name in order:
        items = buckets.get(name, [])
        if not items:
            continue
        lines.append(f"## {name}（{len(items)}）")
        lines.append("")
        lines.append("| 方法 | 路径 | 处理器 |")
        lines.append("| --- | --- | --- |")
        for r in sorted(items, key=lambda x: (x["path"], x["method"])):
            lines.append(f"| `{r['method']}` | `{r['path']}` | `{r['handler'] or '-'}` |")
        lines.append("")

    OUT.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"wrote {OUT} ({len(unique)} routes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
