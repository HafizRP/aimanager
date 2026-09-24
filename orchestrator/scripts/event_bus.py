#!/usr/bin/env python3
import argparse
import json
import os
import sys
import time
from pathlib import Path

def get_dirs(base_dir):
    bus_dir = Path(base_dir) / "bus"
    events_dir = bus_dir / "events"
    inbox_dir = bus_dir / "inboxes"
    events_dir.mkdir(parents=True, exist_ok=True)
    inbox_dir.mkdir(parents=True, exist_ok=True)
    return bus_dir, events_dir, inbox_dir

def emit_event(base_dir, sender, event_name, payload_str):
    _, events_dir, _ = get_dirs(base_dir)
    timestamp = int(time.time() * 1000)
    event_file = events_dir / f"{timestamp}_{event_name}.json"
    
    payload = {}
    if payload_str:
        try:
            payload = json.loads(payload_str)
        except Exception:
            payload = {"text": payload_str}
            
    data = {
        "event": event_name,
        "from": sender,
        "timestamp": timestamp,
        "payload": payload
    }
    
    with open(event_file, "w") as f:
        json.dump(data, f, indent=2)
        
    print(f"[EVENT_EMITTED] {event_name} from {sender}")
    return 0

def has_event(base_dir, event_name):
    _, events_dir, _ = get_dirs(base_dir)
    for f in events_dir.glob("*_*.json"):
        name = f.stem.split("_", 1)[1] if "_" in f.stem else ""
        if name == event_name:
            return True
    return False

def wait_event(base_dir, event_name, timeout=300, interval=1.0):
    start = time.time()
    _, events_dir, _ = get_dirs(base_dir)
    
    while time.time() - start < timeout:
        for f in sorted(events_dir.glob(f"*_{event_name}.json")):
            try:
                with open(f, "r") as ef:
                    content = json.load(ef)
                print(json.dumps(content))
                return 0
            except Exception:
                pass
        time.sleep(interval)
        
    print(f"[TIMEOUT] Event {event_name} not received within {timeout}s", file=sys.stderr)
    return 1

def send_message(base_dir, sender, recipient, msg_type, body_str):
    _, _, inbox_dir = get_dirs(base_dir)
    target_inbox = inbox_dir / recipient
    target_inbox.mkdir(parents=True, exist_ok=True)
    
    timestamp = int(time.time() * 1000)
    msg_file = target_inbox / f"{timestamp}_{msg_type}.json"
    
    body = {}
    if body_str:
        try:
            body = json.loads(body_str)
        except Exception:
            body = {"text": body_str}
            
    data = {
        "type": msg_type,
        "from": sender,
        "to": recipient,
        "timestamp": timestamp,
        "body": body
    }
    
    with open(msg_file, "w") as f:
        json.dump(data, f, indent=2)
        
    print(f"[MESSAGE_SENT] {msg_type} from {sender} -> {recipient}")
    return 0

def read_inbox(base_dir, agent_name, consume=False):
    _, _, inbox_dir = get_dirs(base_dir)
    target_inbox = inbox_dir / agent_name
    if not target_inbox.exists():
        print("[]")
        return 0
        
    messages = []
    files_to_delete = []
    
    for f in sorted(target_inbox.glob("*.json")):
        try:
            with open(f, "r") as mf:
                messages.append(json.load(mf))
            if consume:
                files_to_delete.append(f)
        except Exception:
            pass
            
    for f in files_to_delete:
        f.unlink(missing_ok=True)
        
    print(json.dumps(messages))
    return 0

def main():
    parser = argparse.ArgumentParser(description="Inter-Agent Event & Message Bus")
    parser.add_argument("--base-dir", default=".", help="Base project directory")
    subparsers = parser.add_subparsers(dest="command")
    
    # emit
    p_emit = subparsers.add_parser("emit")
    p_emit.add_argument("--from", dest="sender", required=True)
    p_emit.add_argument("--event", required=True)
    p_emit.add_argument("--payload", default="")
    
    # check
    p_check = subparsers.add_parser("check")
    p_check.add_argument("--event", required=True)
    
    # wait
    p_wait = subparsers.add_parser("wait")
    p_wait.add_argument("--event", required=True)
    p_wait.add_argument("--timeout", type=int, default=300)
    p_wait.add_argument("--interval", type=float, default=1.0)
    
    # send
    p_send = subparsers.add_parser("send")
    p_send.add_argument("--from", dest="sender", required=True)
    p_send.add_argument("--to", required=True)
    p_send.add_argument("--type", required=True)
    p_send.add_argument("--body", default="")
    
    # inbox
    p_inbox = subparsers.add_parser("inbox")
    p_inbox.add_argument("--agent", required=True)
    p_inbox.add_argument("--consume", action="store_true")
    
    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(1)
        
    if args.command == "emit":
        sys.exit(emit_event(args.base_dir, args.sender, args.event, args.payload))
    elif args.command == "check":
        sys.exit(0 if has_event(args.base_dir, args.event) else 1)
    elif args.command == "wait":
        sys.exit(wait_event(args.base_dir, args.event, args.timeout, args.interval))
    elif args.command == "send":
        sys.exit(send_message(args.base_dir, args.sender, args.to, args.type, args.body))
    elif args.command == "inbox":
        sys.exit(read_inbox(args.base_dir, args.agent, args.consume))

if __name__ == "__main__":
    main()
