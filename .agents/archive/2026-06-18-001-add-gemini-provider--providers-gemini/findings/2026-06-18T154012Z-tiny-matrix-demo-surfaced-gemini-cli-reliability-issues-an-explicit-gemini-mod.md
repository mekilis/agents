# Finding: Tiny matrix demo surfaced Gemini CLI reliability issues: an explicit `gemini_mod

Status: fixed
Severity: medium
Created by: smart

Tiny matrix demo surfaced Gemini CLI reliability issues: an explicit `gemini_model: gemini-3-flash` failed with ModelNotFoundError, and provider-default Gemini retried on 429 `MODEL_CAPACITY_EXHAUSTED` for `gemini-3-flash-preview` until interrupted. Antigravity completed the implementer role successfully in the agy->gemini permutation. Consider documenting Gemini capacity risk and adding a bounded timeout/retry strategy before treating Gemini as a reliable reviewer in automated loops.

## Fixed

By: smart
Recorded at: 2026-06-18T16:12:36Z
Evidence: README now recommends agy for Google-side provider runs, documents gemini-2.5-flash-lite as the explicit Gemini fallback, and warns that Gemini CLI headless tool limits do not count as passing verification. Agents also filters manual/non-executable agents-verify directives so Gemini cannot accidentally advance the approval boundary with fake command evidence.
