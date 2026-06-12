# Handoff: implementer

To: implementer
From: smart

Let the reviewer role accept either a scalar provider or an ordered list of reviewer providers. Target config: roles.reviewer: [lmstudio, codex] or YAML list form. Remove review.first_pass from the primary model; every list item is a reviewer for the reviewer role. Keep scalar roles compatible, support future map list items where practical, add tests for parsing, routing, local reviewer findings, clean local reviewer continuing to the next reviewer, and final verification behavior.
