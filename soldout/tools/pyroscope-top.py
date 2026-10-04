#!/usr/bin/env python3
"""Преобразует flamebearer-JSON Pyroscope (/pyroscope/render) в таблицу вида `go tool pprof -top`: self/cum по функциям."""
import json
import sys
from collections import defaultdict


def main(path, limit=30):
    fb = json.load(open(path))["flamebearer"]
    names, levels = fb["names"], fb["levels"]
    total = fb.get("numTicks") or (levels[0][1] if levels else 0)
    self_t, cum_t = defaultdict(int), defaultdict(int)
    for level in levels:
        offset = 0
        for i in range(0, len(level), 4):
            delta, tot, self_, name = level[i], level[i + 1], level[i + 2], level[i + 3]
            offset += delta
            self_t[names[name]] += self_
            cum_t[names[name]] += tot
            offset += tot
    rows = sorted(self_t.items(), key=lambda kv: -kv[1])[:limit]
    unit = fb.get("units", "samples")
    print(f"Источник: Pyroscope, {fb.get('spyName', 'gospy')}; всего {total} {unit}")
    print(f"{'flat':>12} {'flat%':>7} {'cum':>12} {'cum%':>7}  функция")
    for name, s in rows:
        c = cum_t[name]
        print(f"{s:>12} {100 * s / total if total else 0:>6.2f}% {c:>12} {100 * c / total if total else 0:>6.2f}%  {name}")


if __name__ == "__main__":
    main(sys.argv[1], int(sys.argv[2]) if len(sys.argv) > 2 else 30)
