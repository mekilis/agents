# Handoff: reviewer

To: reviewer
From: implementer

Implemented optional local AI coordinator advisory preflight for coordinator prompts. Reused local AI config/client flow, validates and normalizes JSON advisory output, omits invalid/unavailable output, and filters advisory text that attempts role routing or handoff creation. Added tests covering valid advisory inclusion, invalid omission, and role-boundary preservation. Full check passed: GOCACHE=$(mktemp -d /tmp/agents-gocache-XXXXXX) go test ./...
