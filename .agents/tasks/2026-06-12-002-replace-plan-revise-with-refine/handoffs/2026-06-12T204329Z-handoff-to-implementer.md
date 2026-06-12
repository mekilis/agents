# Handoff: implementer

To: implementer
From: smart

Fix the open finding: /plan refine must not append a visible 'feedback to apply' block or generic scaffold guidance to plan.md. It should produce a refined durable free-form plan or fail honestly if it lacks enough information. Preserve the requirement that /plan revise is removed with no alias/fallback. Update tests so they reject meta-note/scaffold persistence and confirm feedback is folded into plan content.
