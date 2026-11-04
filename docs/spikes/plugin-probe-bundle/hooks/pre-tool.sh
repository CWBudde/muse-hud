#!/bin/sh
# Phase 4 spike probe (PreToolUse). NOT installed, NOT run by the spike.
# A freshness measurement variant would timestamp each invocation to a
# user-chosen file so hook latency could be compared with session.jsonl
# tailing; the shipped scaffold deliberately records nothing (hook
# stdin may carry session content, and muse-hud never copies that).
cat >/dev/null
exit 0
