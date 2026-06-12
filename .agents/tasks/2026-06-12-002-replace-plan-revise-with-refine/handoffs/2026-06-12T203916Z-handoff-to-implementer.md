# Handoff: implementer

To: implementer
From: smart

Replace /plan revise with /plan refine in interactive and CLI plan commands. Remove revise entirely rather than keeping it as a fallback or alias. Refine should improve the durable active task plan from user feedback/context instead of behaving like an ad hoc text setter. Keep plan Markdown free-form and substantial. Update help/docs/tests. Preserve scriptable non-interactive behavior where relevant, but no hidden compatibility fallback for revise.
