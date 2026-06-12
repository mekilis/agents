# Handoff: coordinator

To: coordinator
From: smart

Coordinate this reviewer-role-list change before implementation. Desired product grammar: roles.reviewer can be a scalar provider for one reviewer or a YAML list for ordered reviewer providers, e.g. roles: reviewer: [lmstudio, codex] or reviewer:\n  - lmstudio\n  - codex. Avoid review.first_pass as primary config. Every list item performs the reviewer role in sequence. Preserve old roles.reviewer scalar behavior and provider map settings. Produce a concrete implementer scope with compatibility and tests.
