# Finding: agents verify --cmd executes the evidence command before checking for an active

Status: fixed
Severity: medium
Created by: cursor
Agent: cursor (model fable-5)

agents verify --cmd executes the evidence command before checking for an active task, so arbitrary commands run even when recording must fail

## Fixed

By: cursor
Agent: cursor (model fable-5)
Recorded at: 2026-06-10T11:28:00Z
Evidence: addVerification now checks readActiveTask before executing --cmd; TestVerifyCmdRequiresActiveTaskBeforeRunningCommand proves the command never runs
