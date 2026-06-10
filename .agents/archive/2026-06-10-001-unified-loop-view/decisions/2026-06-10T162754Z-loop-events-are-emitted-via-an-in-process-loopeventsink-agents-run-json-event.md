# Decision: Loop events are emitted via an in-process loopEventSink; agents run --json-event

Status: proposed
By: implementer

Loop events are emitted via an in-process loopEventSink; agents run --json-events makes stdout a JSONL stream of state_changed, run_started, output, run_finished, stopped events with run ids; plain run output is unchanged
