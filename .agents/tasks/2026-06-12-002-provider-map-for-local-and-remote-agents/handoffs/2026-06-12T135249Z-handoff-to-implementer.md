# Handoff: implementer

To: implementer
From: smart

Redesign role/provider configuration so providers are named by who runs them (lmstudio, ollama, codex, cursor, claude) and models are settings under providers. Remove ambiguous global local_ai_enabled in favor of explicit provider references such as roles.coordinator: lmstudio and review.first_pass: lmstudio. Preserve backward compatibility where reasonable and add tests for config parsing, role resolution, and reviewer first-pass behavior.
