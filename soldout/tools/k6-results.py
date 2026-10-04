#!/usr/bin/env python3
"""Таблица результатов k6 из k6/results/*.json (--summary-export): шаг → RPS, p50/p95/p99, ошибки."""
import glob
import json
import os
import sys


def pick(metrics, name, key):
    m = metrics.get(name) or {}
    v = m.get(key)
    return v


def fmt_ms(v):
    return "-" if v is None else f"{v:.0f}"


def main(path):
    rows = []
    for f in sorted(glob.glob(os.path.join(path, "*.json")), key=os.path.getmtime):
        try:
            d = json.load(open(f))
        except Exception:  # noqa: BLE001
            continue
        m = d.get("metrics", {})
        if "http_reqs" not in m:  # не k6 summary (например, выгрузка Pyroscope)
            continue
        name = os.path.basename(f)[:-5]
        reqs = m.get("http_reqs", {})
        failed = m.get("http_req_failed", {})
        hold = m.get("http_req_duration{name:hold}", {})
        seat = m.get("http_req_duration{name:seatmap}", {})
        anyh = m.get("http_req_duration{name:hold_any}", {})
        rows.append([
            name,
            f"{reqs.get('rate', 0):.0f}",
            f"{100 * failed.get('value', 0):.1f}%",
            fmt_ms(hold.get("p(50)")), fmt_ms(hold.get("p(95)")), fmt_ms(hold.get("p(99)")),
            fmt_ms(seat.get("p(99)")), fmt_ms(anyh.get("p(99)")),
        ])
    head = ["прогон", "RPS", "ошибки", "hold p50", "hold p95", "hold p99", "seatmap p99", "hold/any p99"]
    widths = [max(len(str(r[i])) for r in rows + [head]) for i in range(len(head))]
    line = "| " + " | ".join(h.ljust(w) for h, w in zip(head, widths)) + " |"
    print(line)
    print("|" + "|".join("-" * (w + 2) for w in widths) + "|")
    for r in rows:
        print("| " + " | ".join(str(c).ljust(w) for c, w in zip(r, widths)) + " |")


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else "k6/results")
