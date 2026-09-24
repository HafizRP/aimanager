import sys, re, json

def extract_valid_json(text):
    # Match markdown json block first
    code_block = re.search(r'```(?:json)?\s*([\s\S]*?)\s*```', text)
    if code_block:
        candidate = code_block.group(1).strip()
        try:
            return json.loads(candidate)
        except:
            pass

    # Regex search for outer-most brackets
    json_candidates = re.findall(r'(\{[\s\S]*\})', text)
    for cand in reversed(json_candidates):
        try:
            return json.loads(cand)
        except:
            continue
    return None

if __name__ == "__main__":
    raw = sys.stdin.read()
    parsed = extract_valid_json(raw)
    if parsed:
        print(json.dumps(parsed))
        sys.exit(0)
    else:
        sys.exit(1)
