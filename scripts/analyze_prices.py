#!/usr/bin/env python3
"""
analyze_prices.py — разведка прайсов (task 0).

Сканирует XML-прайсы (Nokaut-формат) в ./prices/{company}/*.xml и собирает:
  - корневой тег и число офферов
  - какие поля есть в каждом оффере
  - именованные <property name="..."> vs анонимные <property>
  - распределение длин EAN и примеры
  - сравнение PreviousPrice vs price (сколько скидок)
  - формат цен (запятая/точка)
  - значения availability (для маппинга в конфиге)
  - дополнительные атрибуты (Material и т.п.)

Запуск:  python3 scripts/analyze_prices.py [dir]
Вывод:   stdout-отчёт + prices-report.json
"""
import xml.etree.ElementTree as ET
import os, re, sys, json
from collections import Counter, defaultdict

def parse_price(s):
    if s is None:
        return 0.0
    s = s.strip().replace(" ", "").replace("\u00a0", "")
    if s == "":
        return 0.0
    if "," in s and "." not in s:
        s = s.replace(",", ".")
    elif "," in s and "." in s:
        if s.rfind(",") > s.rfind("."):
            s = s.replace(".", "").replace(",", ".")
        else:
            s = s.replace(",", "")
    try:
        return float(s)
    except ValueError:
        return 0.0

def analyze_file(path, limit=5000):
    tree = ET.parse(path)
    root = tree.getroot()
    offers_el = root.find("offers")
    if offers_el is None:
        return None
    offers = list(offers_el.findall("offer"))[:limit]
    if not offers:
        return {"offers": 0}

    fields = Counter()
    named_props = Counter()
    anon_props = 0
    ean_lens = Counter()
    ean_samples = []
    price_comma = 0
    price_samples = []
    prev_gt = prev_eq = prev_lt = prev_missing = 0
    avail = Counter()
    extra_attrs = Counter()
    no_image = 0
    no_name = 0
    cats = Counter()

    for o in offers:
        d = {}
        for c in o:
            fields[c.tag] += 1
            if c.tag == "property":
                n = c.attrib.get("name")
                if n:
                    named_props[n] += 1
                    d[n] = (c.text or "").strip()
                else:
                    anon_props += 1
            else:
                d[c.tag] = (c.text or "").strip()
        if not d.get("name"):
            no_name += 1
        if not d.get("image"):
            no_image += 1
        e = d.get("EAN", "")
        if e:
            ean_lens[len(e)] += 1
            if len(ean_samples) < 6:
                ean_samples.append(e)
        p = d.get("price", "")
        if "," in p:
            price_comma += 1
        if len(price_samples) < 6:
            price_samples.append(p)
        pp = d.get("PreviousPrice", "")
        if pp == "":
            prev_missing += 1
        else:
            pf, ppf = parse_price(p), parse_price(pp)
            if ppf > pf:
                prev_gt += 1
            elif ppf == pf:
                prev_eq += 1
            else:
                prev_lt += 1
        avail[(d.get("availability", "") or "?")] += 1
        # extra attrs: named props not in the standard set
        standard = {"ProductUrl", "EAN", "ImageOriginalUrl", "Producent",
                    "ShopProductId", "ShopProductCategory", "PreviousPrice"}
        for k in named_props:
            if k not in standard:
                extra_attrs[k] += 1
        sc = d.get("shopcategory", "")
        if sc:
            cats[sc] += 1

    return {
        "offers": len(offers),
        "fields": dict(fields),
        "named_props": dict(named_props),
        "anon_props": anon_props,
        "ean_lens": dict(ean_lens),
        "ean_samples": ean_samples,
        "price_comma": price_comma,
        "price_samples": price_samples,
        "prev_gt": prev_gt, "prev_eq": prev_eq, "prev_lt": prev_lt,
        "prev_missing": prev_missing,
        "availability": dict(avail),
        "extra_attrs": dict(extra_attrs),
        "no_image": no_image, "no_name": no_name,
        "top_shopcategories": cats.most_common(8),
    }

def main():
    base = sys.argv[1] if len(sys.argv) > 1 else "prices"
    report = {}
    for d in sorted(os.listdir(base)):
        dp = os.path.join(base, d)
        if not os.path.isdir(dp):
            continue
        for f in sorted(os.listdir(dp)):
            if not f.endswith(".xml"):
                continue
            r = analyze_file(os.path.join(dp, f))
            report[d] = r

    # Print human-readable
    print("=" * 70)
    print("PRICE FILES ANALYSIS REPORT")
    print("=" * 70)
    total_offers = 0
    for d, r in report.items():
        if not r or r.get("offers", 0) == 0:
            print(f"\n--- {d}: EMPTY (0 offers)")
            continue
        n = r["offers"]
        total_offers += n
        print(f"\n=== {d}  (offers={n})")
        print(f"  root fields: {r['fields']}")
        if r["named_props"]:
            print(f"  named props: {r['named_props']}")
        if r["anon_props"]:
            print(f"  anon props: {r['anon_props']}")
        print(f"  EAN lens: {r['ean_lens']}  samples={r['ean_samples']}")
        print(f"  prices (comma={r['price_comma']}): {r['price_samples']}")
        print(f"  PreviousPrice > price: {r['prev_gt']}  (== {r['prev_eq']}, < {r['prev_lt']}, missing {r['prev_missing']})")
        print(f"  availability: {r['availability']}")
        if r["extra_attrs"]:
            print(f"  extra attrs: {r['extra_attrs']}")
        if r["no_image"] or r["no_name"]:
            print(f"  no_image={r['no_image']} no_name={r['no_name']}")
        if r["top_shopcategories"]:
            print(f"  top shopcategories: {r['top_shopcategories']}")

    print("\n" + "=" * 70)
    print(f"TOTAL offers across all files: {total_offers}")
    print("=" * 70)

    # Save JSON
    out = os.path.join(os.path.dirname(base) if os.path.isdir(base) else ".", "prices-report.json")
    try:
        with open(out, "w", encoding="utf-8") as fh:
            json.dump(report, fh, ensure_ascii=False, indent=2)
        print(f"\nSaved JSON report to: {out}")
    except Exception as e:
        print(f"\nWARN: could not save JSON report: {e}")

if __name__ == "__main__":
    main()
