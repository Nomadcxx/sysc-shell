#!/usr/bin/env python3
"""Block Go race/trace/full-tree tests that OOM this workstation."""
import json
import re
import sys

GO_INVOKE = re.compile(
    r"\bgo\s+(?:test|build|run|install)\b", re.IGNORECASE
)
RACE_FLAG = re.compile(r"(?:^|\s)-race(?:\s|$)", re.MULTILINE)
TOOL_TRACE = re.compile(r"\bgo\s+tool\s+trace\b", re.IGNORECASE)
FULL_TREE = re.compile(r"\bgo\s+test\b[^\n;|&]*\./\.\.\.", re.IGNORECASE)
COUNT = re.compile(r"\bgo\s+test\b[^\n;|&]*-count=(\d+)", re.IGNORECASE)
ALLOCFREE = re.compile(r"\ballocfreetrace\b", re.IGNORECASE)
DOC_ONLY = re.compile(
    r"\b(?:rg|grep|ag|ack|git\s+grep|git\s+log|git\s+show)\b", re.IGNORECASE
)


def command_from(payload):
    if isinstance(payload, str):
        try:
            payload = json.loads(payload)
        except json.JSONDecodeError:
            return payload
    if not isinstance(payload, dict):
        return ""
    for key in ("command", "cmd"):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            return value
    for key in ("tool_input", "arguments", "input", "params"):
        inner = payload.get(key)
        found = command_from(inner) if inner else ""
        if found:
            return found
    return ""


def deny_reason(cmd):
    if not cmd:
        return None
    doc = bool(DOC_ONLY.search(cmd))
    go = bool(GO_INVOKE.search(cmd))
    if ALLOCFREE.search(cmd) and not doc:
        return (
            "GODEBUG=allocfreetrace is banned on this machine (OOM). "
            "Do not trace alloc/free."
        )
    if TOOL_TRACE.search(cmd) and not (doc and not go):
        return "go tool trace is banned on this machine (OOM). Do not profile here."
    if go and RACE_FLAG.search(cmd):
        if doc and not re.search(
            r"\bgo\s+(?:test|build|run|install)\b[^\n;|&]*-race", cmd, re.I
        ):
            return None
        return (
            "go test/build -race OOMs this machine. Use: "
            "timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>"
        )
    if FULL_TREE.search(cmd):
        if doc and not re.search(r"\bgo\s+test\b[^\n;|&]*\./\.\.\.", cmd, re.I):
            return None
        return (
            "go test ./... OOMs this machine. Test one package with "
            "timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>"
        )
    match = COUNT.search(cmd)
    if match and int(match.group(1)) > 1:
        if doc and not go:
            return None
        return "go test -count>1 is banned here. Use -count=1."
    return None


def main():
    raw = sys.stdin.read()
    try:
        payload = json.loads(raw) if raw.strip() else {}
    except json.JSONDecodeError:
        print(json.dumps({"permission": "allow"}))
        return
    why = deny_reason(command_from(payload))
    if why:
        print(
            json.dumps(
                {
                    "permission": "deny",
                    "user_message": why,
                    "agent_message": why
                    + " Never retry with -race, ./..., go tool trace, or allocfreetrace.",
                }
            )
        )
        return
    print(json.dumps({"permission": "allow"}))


if __name__ == "__main__":
    main()
