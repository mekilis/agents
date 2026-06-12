# Handoff: implementer

To: implementer
From: coordinator

Implement the smallest safe increment for local AI advanced coordination: add an optional local AI coordinator preflight/advisory that can enrich the configured coordinator prompt without replacing roles or auto-routing work. Start from internal/app/local_ai.go and internal/app/coordinator.go. Reuse the existing local AI config/client patterns. The advisory should be included only when local_ai_enabled is configured and the result is valid; failures or malformed results should fall back to the current prompt behavior. Preserve configured role boundaries: local AI must not replace roles.coordinator and must not create implementer/reviewer handoffs. Add focused tests covering valid advisory inclusion, invalid/unavailable omission, and role boundary preservation. Run GOCACHE=<isolated-cache> go test ./... if feasible.
