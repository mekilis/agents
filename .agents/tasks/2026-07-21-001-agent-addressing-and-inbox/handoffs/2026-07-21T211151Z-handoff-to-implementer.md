# Handoff: implementer

To: implementer
From: smart

Fix all three open reviewer findings: validate addressed handoff agent names against the active task registry, reject unsafe registered agent names before they can affect paths, and add deterministic focused watch emission coverage that proves existing inbox items are not replayed and one new matching unconsumed item produces one stable JSON event. Mark each finding fixed with concrete evidence, rerun the isolated-cache full suite, and hand off for independent review.
