#!/usr/bin/env python3
"""Fitness function (занятие 2): каждый SQL-литерал в internal/<module>/ начинается с /* <module>.<name> */
и ссылается только на таблицы своего модуля (docs/table-owners.yml). Выход 1 при нарушении.
Использование: tools/check-sql-owners.py [--root soldout] [--owners ../docs/table-owners.yml] [paths...]"""
import argparse
import os
import re
import sys

try:
    import yaml  # type: ignore
except ImportError:  # минимальный парсер для нашего простого YAML
    yaml = None

SQL_RE = re.compile(r'`([^`]*)`|"((?:[^"\\]|\\.)*)"', re.S)
KW_RE = re.compile(r'\b(?:FROM|JOIN|INTO|UPDATE|DELETE\s+FROM|TABLE)\s+(?:ONLY\s+)?"?([a-z_][a-z0-9_]*)"?', re.I)
PREFIX_RE = re.compile(r'^\s*/\*\s*(?:([a-z_]+)\.([a-zA-Z0-9_.-]+)|(%s))\s*\*/', re.S)
SQL_HINT = re.compile(r'\b(SELECT|INSERT|UPDATE|DELETE|WITH)\b', re.I)
# запрос — литерал, который начинается с комментария или SQL-ключевого слова; хвосты конкатенации (" FROM holds …") пропускаем
QUERY_START = re.compile(r'^\s*(/\*|SELECT\b|INSERT\b|UPDATE\b|DELETE\b|WITH\b)', re.I | re.S)
SYSTEM = {"pg_stat_statements", "pg_stat_activity", "pg_index", "pg_extension", "generate_series", "unnest", "pg_settings", "ONLY"}


def load_owners(path):
    if yaml:
        return yaml.safe_load(open(path))
    owners, exceptions, section = {}, [], None
    for line in open(path):
        line = line.split("#")[0].rstrip()
        if not line.strip():
            continue
        if line.startswith("owners:"):
            section = "owners"
        elif line.startswith("exceptions:"):
            section = "exceptions"
        elif section == "owners" and ":" in line:
            k, v = line.strip().split(":", 1)
            owners[k] = [t.strip() for t in v.strip().strip("[]").split(",") if t.strip()]
        elif section == "exceptions" and "path:" in line:
            exceptions.append({"path": line.split("path:")[1].strip()})
    return {"owners": owners, "exceptions": exceptions}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=".")
    ap.add_argument("--owners", default="../docs/table-owners.yml")
    ap.add_argument("paths", nargs="*")
    a = ap.parse_args()
    cfg = load_owners(os.path.join(a.root, a.owners) if not os.path.isabs(a.owners) else a.owners)
    table_owner = {t: m for m, ts in cfg["owners"].items() for t in ts}
    exc = [e["path"] for e in cfg.get("exceptions", [])]
    roots = a.paths or [os.path.join(a.root, "internal")]
    problems = []
    for root in roots:
        for dirpath, _, files in os.walk(root):
            rel = os.path.relpath(dirpath, a.root)
            if any(rel.startswith(e.rstrip("/")) for e in exc) or "testdata" in rel and "red" not in rel:
                continue
            for fn in files:
                if not fn.endswith(".go") or fn.endswith("_test.go"):
                    continue
                path = os.path.join(dirpath, fn)
                src = open(path, encoding="utf-8").read()
                m = re.search(r"internal/([a-z]+)/", path.replace(os.sep, "/"))
                module = m.group(1) if m else None
                for lit in SQL_RE.finditer(src):
                    sql = lit.group(1) or lit.group(2) or ""
                    if not SQL_HINT.search(sql) or len(sql) < 12 or not QUERY_START.match(sql):
                        continue
                    tables = {t for t in KW_RE.findall(sql) if t not in SYSTEM and t in table_owner}
                    if not tables and not PREFIX_RE.match(sql):
                        continue  # не SQL к нашим таблицам
                    pm = PREFIX_RE.match(sql)
                    where = f"{os.path.relpath(path, a.root)}:{src[:lit.start()].count(chr(10)) + 1}"
                    if not pm:
                        problems.append(f"{where}: нет префикса /* module.name */: {sql.strip()[:70]}…")
                        continue
                    prefix_module = pm.group(1) or module or "platform"
                    if module and prefix_module != module and module != "platform":
                        problems.append(f"{where}: префикс {prefix_module}.* в модуле {module}")
                    for t in tables:
                        owner = table_owner[t]
                        if prefix_module not in (owner, "invariants", "test", "seed", "platform") and owner != "platform":
                            problems.append(f"{where}: модуль {prefix_module} обращается к таблице {t} (владелец {owner})")
    for p in problems:
        print(p)
    if problems:
        print(f"\nНАРУШЕНИЙ: {len(problems)}")
        sys.exit(1)
    print("SQL-литералы: префиксы и владение таблицами в порядке.")


if __name__ == "__main__":
    main()
