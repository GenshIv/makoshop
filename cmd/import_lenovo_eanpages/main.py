#!/usr/bin/env python3
"""
Import Lenovo PSREF catalog into Makoshop EAN pages with shard compaction.

Reads lenovo-psref-catalog.json and creates/updates EAN pages via the API
with unique, human-like Polish descriptions based on product specs. Checks
database shard health after every 10 products and triggers compaction if needed.
"""

import json
import random
import re
import string
import sys
import time
from urllib.request import Request, urlopen
from urllib.error import HTTPError
from urllib.parse import quote

API_BASE = "http://localhost:9090"
JWT_TOKEN = None
PRODUCTS_PROCESSED = 0
LAST_COMPACT_CHECK = 0
COMPACT_INTERVAL = 10
BLAOFT_THRESHOLD_PCT = 60

# Category mapping: Lenovo product type -> Makoshop category ID
CATEGORY_MAP = {
    "Laptops": 20,       # Laptopy
    "Tablets": 21,       # Tablety
    "Desktops & AIOs": 390,  # Komputery stacjonarne
}

# Default category if not matched
DEFAULT_CATEGORY_ID = 20


def get_jwt_token():
    global JWT_TOKEN
    if JWT_TOKEN:
        return JWT_TOKEN
    
    # Try registering a new user first
    try:
        data = json.dumps({"email": "lenovo-import@example.com", "password": "import123", "role": "admin"}).encode()
        req = Request(f"{API_BASE}/auth/register", data=data, headers={"Content-Type": "application/json"})
        resp = urlopen(req)
        result = json.loads(resp.read().decode())
        JWT_TOKEN = result["token"]
        return JWT_TOKEN
    except HTTPError as e:
        if e.code == 409:
            # User exists, try login
            pass
        else:
            print(f"Register failed: {e.code} - {e.read().decode()}")
    
    # Try login
    data = json.dumps({"email": "lenovo-import@example.com", "password": "import123"}).encode()
    req = Request(f"{API_BASE}/auth/login", data=data, headers={"Content-Type": "application/json"})
    try:
        resp = urlopen(req)
        result = json.loads(resp.read().decode())
        JWT_TOKEN = result["token"]
        return JWT_TOKEN
    except HTTPError as e:
        print(f"Login failed: {e.code} - {e.read().decode()}")
        sys.exit(1)


def api_request(method, path, data=None):
    token = get_jwt_token()
    url = f"{API_BASE}{path}"
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
        "Accept": "application/json"
    }
    body = json.dumps(data).encode() if data else None
    req = Request(url, data=body, headers=headers, method=method)
    try:
        resp = urlopen(req)
        return json.loads(resp.read().decode())
    except HTTPError as e:
        body = e.read().decode()
        if e.code == 409:
            return {"status": "exists"}
        elif e.code == 404:
            return None
        else:
            print(f"API error {e.code} for {path}: {body[:200]}")
            return None


def check_shard_health():
    result = api_request("GET", "/admin/db/shards")
    if not result or not isinstance(result, list):
        return 0

    max_bloat = 0
    for shard in result:
        dead_bytes = shard.get("DeadBytes", 0)
        file_size = shard.get("FileSize", 1)  # Avoid division by zero
        if file_size > 0 and dead_bytes > 0:
            # Bloat = percentage of total file size that is dead bytes
            bloat_pct = (dead_bytes / file_size) * 100
            if bloat_pct > max_bloat:
                max_bloat = bloat_pct

    return max_bloat


def wait_for_maintenance():
    print("  [WAIT] Waiting for maintenance job to complete...")
    while True:
        progress = api_request("GET", "/admin/import-progress")
        if progress and not progress.get("running", False):
            status = progress.get("status", "unknown")
            if status == "failed":
                print("  [WARN] Last maintenance job failed!")
            else:
                print(f"  [OK] Maintenance job completed (status: {status})")
            return True
        time.sleep(5)


def trigger_compact():
    if is_maintenance_running():
        print("  [COMPACT] Maintenance already running, waiting...")
        wait_for_maintenance()
        return True

    print("  [COMPACT] Triggering database compaction...")
    result = api_request("POST", "/admin/compact")
    if not result:
        print("  [COMPACT] Failed to trigger compact")
        return False

    print("  [COMPACT] Waiting for completion...")
    wait_for_maintenance()
    return True


def is_maintenance_running():
    progress = api_request("GET", "/admin/import-progress")
    if not progress:
        return False
    return progress.get("running", False)


def maybe_compact(force=False):
    global PRODUCTS_PROCESSED, LAST_COMPACT_CHECK

    if force or (PRODUCTS_PROCESSED - LAST_COMPACT_CHECK) >= COMPACT_INTERVAL:
        LAST_COMPACT_CHECK = PRODUCTS_PROCESSED

        print(f"  [HEALTH] Checking shard health after {PRODUCTS_PROCESSED} products...")
        max_bloat = check_shard_health()
        print(f"  [HEALTH] Max bloat: {max_bloat:.1f}%")

        if max_bloat > BLAOFT_THRESHOLD_PCT:
            trigger_compact()


def get_category_id(model_type):
    """Map Lenovo model type to Makoshop category ID."""
    for key, cat_id in CATEGORY_MAP.items():
        if key.lower() in model_type.lower():
            return cat_id
    return DEFAULT_CATEGORY_ID


def generate_slug(text):
    text = text.lower()
    text = re.sub(r'[^\w\s-]', '', text)
    text = re.sub(r'[\s_]+', '-', text)
    text = text.strip('-')
    suffix = ''.join(random.choices(string.ascii_lowercase + string.digits, k=4))
    return f"{text}-{suffix}"


def generate_polish_description(product_name, specs, highlights):
    """Generate unique, human-like Polish description based on product specs."""

    processor = specs.get("Processor", "")
    memory = specs.get("Memory", "")
    storage = specs.get("Storage", "")
    display = specs.get("Display", "")
    weight = specs.get("Weight", "")
    battery = specs.get("Battery", "")
    os_name = specs.get("Operating System", "")
    graphics = specs.get("Graphics", "")
    camera = specs.get("Camera", "")
    keyboard = specs.get("Keyboard", "")
    speakers = specs.get("Speakers", "")
    wifi = specs.get("WLAN + Bluetooth", "")

    # Extract processor info for natural mentions
    proc_info = ""
    if processor:
        proc_short = re.sub(r',.*$', '', processor)
        proc_info = proc_short[:60]

    # Build description organically based on available specs
    paragraphs = []

    # Opening - vary based on product type
    if "ThinkPad" in product_name or "ThinkCentre" in product_name:
        paragraphs.append(f"{product_name} to profesjonalne urządzenie stworzone dla wymagających użytkowników biznesowych. Połączenie niezawodności, wydajności i eleganckiej konstrukcji sprawia, że jest idealnym towarzyszem w codziennej pracy.")
    elif "IdeaPad" in product_name or "IdeaCentre" in product_name:
        paragraphs.append(f"{product_name} łączy nowoczesny design z praktyczną funkcjonalnością. To urządzenie zaprojektowano z myślą o użytkownikach, którzy cenią sobie zarówno wygląd, jak i niezawodność w codziennym użytkowaniu.")
    elif "Legion" in product_name:
        paragraphs.append(f"{product_name} to potężna maszyna stworzona dla graczy i entuzjastów wydajności. Zaawansowane komponenty zapewniają płynną pracę nawet przy najbardziej wymagających grach i aplikacjach.")
    elif "Yoga" in product_name:
        paragraphs.append(f"{product_name} to uniwersalne urządzenie, które dostosowuje się do Twoich potrzeb. Dzięki innowacyjnej konstrukcji możesz pracować w pozycji laptopa, tabletu lub namiotu - zawsze wygodnie i efektywnie.")
    else:
        paragraphs.append(f"{product_name} to nowoczesne urządzenie od Lenovo, które łączy w sobie zaawansowane technologie z przystępną ceną. Idealny wybór dla osób poszukujących niezawodnego sprzętu do pracy i rozrywki.")

    # Processor paragraph - only if we have info
    if proc_info:
        paragraphs.append(f"W centrum tego urządzenia znajduje się {proc_info}. Ten procesor zapewnia wystarczającą moc obliczeniową do płynnej obsługi wielu zadań jednocześnie, od codziennych aplikacji po bardziej wymagające programy.")

    # Memory and storage - combine naturally
    if memory or storage:
        parts = []
        if memory:
            parts.append(f"pamięcią {memory}")
        if storage:
            parts.append(f"dyskiem {storage}")
        if parts:
            paragraphs.append(f"Konfiguracja obejmuje {' oraz '.join(parts)}, co gwarantuje szybki dostęp do danych i płynną pracę z wieloma otwartymi aplikacjami.")

    # Display - important for all devices
    if display:
        paragraphs.append(f"Ekran {display} oferuje wyraźny i szczegółowy obraz. Idealny do pracy z dokumentami, oglądania filmów czy edycji zdjęć.")

    # Graphics - only mention if dedicated or interesting
    if graphics and "Integrated" not in graphics:
        paragraphs.append(f"Karta graficzna {graphics} zapewnia doskonałą wydajność w grach i aplikacjach graficznych.")

    # Battery and weight - for portable devices
    if battery and weight:
        paragraphs.append(f"Z baterią o pojemności {battery} i wagą zaledwie {weight}, to urządzenie jest idealne do pracy w podróży. Będziesz mógł pracować przez wiele godzin bez konieczności ładowania.")
    elif battery:
        paragraphs.append(f"Bateria o pojemności {battery} zapewnia długą pracę na jednym ładowaniu.")

    # Connectivity
    if wifi:
        paragraphs.append(f"Urządzenie wyposażono w {wifi}, co zapewnia szybkie i stabilne połączenie z internetem oraz innymi urządzeniami bezprzewodowymi.")

    # Camera - mention if present and interesting
    if camera and "None" not in camera and "No" not in camera:
        paragraphs.append(f"Wbudowana kamera {camera} umożliwia komfortowe uczestnictwo w wideokonferencjach.")

    # Keyboard - mention if backlit or special
    if keyboard and ("Backlit" in keyboard or "背光" in keyboard):
        paragraphs.append(f"Podświetlana klawiatura ułatwia pracę w warunkach słabego oświetlenia.")

    # OS
    if os_name and "Windows" in os_name:
        paragraphs.append(f"System operacyjny {os_name} zapewnia znajome środowisko pracy z dostępem do tysięcy aplikacji.")

    # Highlights - integrate naturally at the end
    if highlights:
        highlight_items = []
        for h in highlights[:2]:
            if len(h) < 100:  # Skip very long highlights
                highlight_items.append(h)
        if highlight_items:
            paragraphs.append("Wyróżniające się cechy tego modelu to: " + "; ".join(highlight_items) + ".")

    return "\n\n".join(paragraphs)


def generate_title(product_name, specs):
    processor = specs.get("Processor", "")
    memory = specs.get("Memory", "")
    storage = specs.get("Storage", "")

    if processor:
        proc_short = re.sub(r',.*$', '', processor)[:40]
    else:
        proc_short = ""

    parts = [product_name]
    if proc_short:
        parts.append(proc_short)
    if memory:
        mem_short = re.sub(r'[^0-9GB]', '', memory)[:10]
        if mem_short:
            parts.append(mem_short)
    if storage:
        stor_short = re.sub(r'[^0-9GBTB]', '', storage)[:12]
        if stor_short:
            parts.append(stor_short)

    return " | ".join(parts)


def specs_to_attributes(specs):
    attrs = []
    attr_map = {
        "Processor": "Procesor",
        "Memory": "Pamięć RAM",
        "Storage": "Dysk twardy",
        "Graphics": "Karta graficzna",
        "Display": "Ekran",
        "Operating System": "System operacyjny",
        "Weight": "Waga",
        "Battery": "Bateria",
        "Dimensions (WxDxH)": "Wymiary",
        "Case Color": "Kolor obudowy",
        "WLAN + Bluetooth": "Wi-Fi i Bluetooth",
        "Ethernet": "Ethernet",
        "Camera": "Kamera",
        "Speakers": "Głośniki",
        "Keyboard": "Klawiatura",
        "Touchpad": "Dotykowy pad"
    }

    for spec_key, value in specs.items():
        if not value or value == "N/A" or value == "None":
            continue
        pl_name = attr_map.get(spec_key, spec_key)
        clean_value = str(value).strip()
        attrs.append({"key": pl_name, "value": clean_value})

    return attrs


def extract_ean(ean_str):
    if not ean_str:
        return None
    ean_str = str(ean_str).strip()
    match = re.search(r'(\d{12,14})', ean_str)
    if match:
        return match.group(1)
    return None


def process_model(model):
    global PRODUCTS_PROCESSED

    product_name = model["product_name"]
    highlights = model.get("highlight_array", [])
    photos = model.get("photos", [])
    category = model.get("category", "Laptops")

    images = []
    for photo in photos[:3]:
        if "src" in photo:
            images.append(photo["src"])

    detailed_specs = model.get("detailedSpecs", {})
    cols = detailed_specs.get("cols", [])
    rows = detailed_specs.get("rows", [])

    if not rows:
        return 0

    # Get category ID based on product type
    cat_id = get_category_id(category)

    processed = 0

    for row in rows:
        specs = {}
        for i, col_name in enumerate(cols):
            if i < len(row):
                specs[col_name] = row[i]

        ean = None
        if "EAN / UPC / JAN" in cols:
            ean_idx = cols.index("EAN / UPC / JAN")
            if ean_idx < len(row) and row[ean_idx]:
                ean = extract_ean(row[ean_idx])

        if not ean:
            continue

        title = generate_title(product_name, specs)
        description = generate_polish_description(product_name, specs, highlights)
        attributes = specs_to_attributes(specs)
        slug = generate_slug(product_name)

        payload = {
            "title": title,
            "description": description,
            "content": description,
            "slug": slug,
            "brand": "Lenovo",
            "images": images,
            "attributes": attributes,
            "is_active": True,
            "currency": "PLN"
        }

        # Upsert EAN page directly (no product creation)
        upsert_payload = {
            "ean": ean,
            "title": title,
            "description": description[:500],
            "content": description,
            "slug": slug,
            "brand": "Lenovo",
            "images": images,
            "attributes": attributes,
            "is_active": True,
            "category_id": cat_id,
            "currency": "PLN"
        }

        result = api_request("POST", "/admin/eanpages", upsert_payload)
        if result:
            processed += 1
            PRODUCTS_PROCESSED += 1
            print(f"  [upserted] {ean[:12]}... {product_name[:30]}")

        maybe_compact()

    return processed


def main():
    global PRODUCTS_PROCESSED

    print("Lenovo PSREF Catalog Import with Shard Compaction")
    print("=" * 60)

    print("\nLoading catalog...")
    with open("/home/ihar/IdeaProjects/makoshop/lenovo-psref-catalog.json", "r") as f:
        catalog = json.load(f)

    models = catalog["models"]
    print(f"Found {len(models)} models")

    total_processed = 0
    for i, model in enumerate(models):
        product_name = model["product_name"]
        print(f"\n[{i+1}/{len(models)}] Processing: {product_name}")

        try:
            processed = process_model(model)
            total_processed += processed
            print(f"  Processed {processed} EAN pages (total: {PRODUCTS_PROCESSED})")
        except Exception as e:
            print(f"  Error processing model: {e}")

    print("\n" + "=" * 60)
    print(f"Import complete! Total EAN pages processed: {total_processed}")


if __name__ == "__main__":
    main()
