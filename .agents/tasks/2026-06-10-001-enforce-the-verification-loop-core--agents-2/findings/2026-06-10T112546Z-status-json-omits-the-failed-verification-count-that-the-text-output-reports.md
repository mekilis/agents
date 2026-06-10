# Finding: status --json omits the failed-verification count that the text output reports

Status: fixed
Severity: low
Created by: cursor
Agent: cursor (model fable-5)

status --json omits the failed-verification count that the text output reports

## Fixed

By: cursor
Agent: cursor (model fable-5)
Recorded at: 2026-06-10T11:28:00Z
Evidence: recordsJSON gained verification_failed (omitempty); TestStatusJSONIncludesFailedVerification
