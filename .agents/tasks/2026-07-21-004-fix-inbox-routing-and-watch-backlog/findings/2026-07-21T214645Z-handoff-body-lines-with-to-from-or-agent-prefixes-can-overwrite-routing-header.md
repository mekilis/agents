# Finding: Handoff body lines with To, From, or Agent prefixes can overwrite routing header

Status: verified
Severity: high
Created by: supervisor

Handoff body lines with To, From, or Agent prefixes can overwrite routing headers because parseInboxHandoff scans beyond the header block

## Fixed

By: implementer
Recorded at: 2026-07-21T21:49:52Z
Evidence: parseInboxHandoff now stops routing-header recognition at the blank line after From; TestParseInboxHandoffOnlyUsesHeaderBlockForRouting passes and covers body/quoted To, From, and Agent lines plus casing.

## Verification

By: reviewer
Agent: codex (model gpt-5.5, reasoning low)
Recorded at: 2026-07-21T21:52:03Z
Evidence: Reviewed parseInboxHandoff and listInboxItems routing use: parser now stops header parsing at the blank line after From, keeps To/Agent/From from the written header block only, preserves case-insensitive names, and leaves body/quoted headers in Text. Focused parser regression and full go test ./... passed with isolated GOCACHE.
