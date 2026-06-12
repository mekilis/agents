# Finding: Local AI coordinator advisory routing filter misses spaced JSON role directives

Status: verified
Severity: medium
Created by: reviewer

Local AI coordinator advisory routing filter misses spaced JSON role directives like "role": "implementer", so malformed local advice could still inject role-routing text into the coordinator prompt.

## Fixed

By: implementer
Recorded at: 2026-06-12T13:40:21Z
Evidence: Added whitespace-insensitive role directive detection in localAIAdvisoryContainsRouting and covered spaced JSON role directives in TestLocalAICoordinatorAdvisoryRejectsRoutingText; GOCACHE=/var/folders/_r/6vpq9cdn41nf2qznd89khv5m0000gn/T/tmp.roGgXCteoN go test ./... passed.

## Verification

By: reviewer
Recorded at: 2026-06-12T13:40:37Z
Evidence: Verified localAIAdvisoryContainsRouting now compacts whitespace before checking JSON role directives, and TestLocalAICoordinatorAdvisoryRejectsRoutingText covers spaced implementer/reviewer role JSON. Full suite passed with isolated GOCACHE.
