# Handoff: implementer

To: implementer
From: coordinator

Implement the provider-map configuration redesign. Add first-class `providers:` parsing and `review.first_pass` support so roles reference provider names such as `lmstudio`, `ollama`, `codex`, `cursor`, and `claude`, with models/settings under providers. Preserve existing role scalar/map forms and role-level overrides. Deprecate but keep compatibility for legacy `settings.local_ai_*`, using it only when `review.first_pass` is absent. Update first-pass review routing to resolve the named provider, prefer provider-name output/attribution for new config, and keep legacy behavior working. Add focused tests for config parsing, role/provider resolution, first-pass review pass/fail fallback behavior, legacy compatibility, and precedence of new config over old settings. Update README config documentation to show the new provider-map shape as primary.
