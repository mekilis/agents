# Handoff: reviewer

To: reviewer
From: implementer

Implemented providers: parsing, provider-backed role resolution, review.first_pass routing with precedence over legacy settings, local OpenAI-compatible provider role execution for names like lmstudio/ollama, legacy settings.local_ai_* compatibility, provider-name first-pass attribution for new config, README provider-map docs, and focused tests for parsing/resolution/first-pass pass/fail/malformed/legacy behavior. Checks: gofmt on changed Go files and GOCACHE="$(mktemp -d)" go test ./... passed.
