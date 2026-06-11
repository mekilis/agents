# Decision: Structured progress directives use JSON inside fenced agents-progress blocks, no

Status: proposed
By: smart

Structured progress directives use JSON inside fenced agents-progress blocks, not XML or ad hoc key-value text. JSON is compact in terminal transcripts, easy to parse with Go's standard library, and extensible for future fields while preserving raw provider logs.
