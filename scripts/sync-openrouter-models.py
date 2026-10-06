#!/usr/bin/env python3
import hmac
import hashlib
import base64
import json
import time
import urllib.request
import sqlite3
import sys

CORE_DIR = "/home/b14/9router-gateway/data/core"
DB_PATH = f"{CORE_DIR}/db/data.sqlite"
SECRET_PATH = f"{CORE_DIR}/jwt-secret"

def get_auth_token():
    with open(SECRET_PATH, "r") as f:
        secret = f.read().strip().encode()

    def b64url(b):
        return base64.urlsafe_b64encode(b).decode().rstrip("=")

    header = b64url(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
    now = int(time.time())
    payload = b64url(json.dumps({"authenticated": True, "iat": now, "exp": now + 86400}).encode())
    to_sign = f"{header}.{payload}".encode()
    sig = b64url(hmac.new(secret, to_sign, hashlib.sha256).digest())
    return f"{header}.{payload}.{sig}"

def main():
    conn = sqlite3.connect(DB_PATH)
    c = conn.cursor()

    # Find openrouter connection id
    c.execute("SELECT id FROM providerConnections WHERE provider='openrouter' AND isActive=1 LIMIT 1;")
    row = c.fetchone()
    if not row:
        print("Error: No active openrouter providerConnection found in data.sqlite")
        sys.exit(1)
    
    conn_id = row[0]
    print(f"Found OpenRouter connection: {conn_id}")

    # Fetch live models from 9router Core
    token = get_auth_token()
    url = f"http://127.0.0.1:20128/api/providers/{conn_id}/models"
    req = urllib.request.Request(url)
    req.add_header("Cookie", f"auth_token={token}")

    try:
        with urllib.request.urlopen(req) as resp:
            data = json.loads(resp.read().decode())
    except Exception as e:
        print(f"Error fetching models from 9router core endpoint: {e}")
        sys.exit(1)

    models = data.get("models", [])
    if not models:
        print("No models returned from OpenRouter")
        sys.exit(1)

    print(f"Fetched {len(models)} models from OpenRouter API")

    # Clean existing openrouter custom models
    c.execute("DELETE FROM kv WHERE scope='customModels' AND key LIKE 'openrouter|%'")

    inserted = 0
    for m in models:
        mid = m.get("id")
        if not mid:
            continue
        
        name = m.get("name") or mid
        arch = m.get("architecture") or {}
        input_mods = arch.get("input_modalities") or []
        supported_params = m.get("supported_parameters") or []

        caps = {}
        if "image" in input_mods:
            caps["vision"] = True
        if m.get("reasoning") or "reasoning" in supported_params or "include_reasoning" in supported_params:
            caps["reasoning"] = True

        val = {
            "providerAlias": "openrouter",
            "id": mid,
            "type": "llm",
            "name": name
        }
        if caps:
            val["caps"] = caps

        key = f"openrouter|{mid}|llm"
        c.execute("INSERT OR REPLACE INTO kv(scope, key, value) VALUES('customModels', ?, ?)", (key, json.dumps(val)))
        inserted += 1

    conn.commit()
    conn.close()

    print(f"Successfully synced {inserted} OpenRouter models into 9router Core database!")

if __name__ == "__main__":
    main()
