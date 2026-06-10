# Handoff: reviewer

To: reviewer
From: cursor
Agent: cursor (model claude-fable-5-thinking-high)

Second pass adds model-qualified attribution (AGENTS_AGENT -> Agent: lines on all records, qualified run/dispatch output) and agents task scope|status|next setters. Review agentDescriptor, agentLine, setTaskChunk in internal/app/app.go; suite green with -race.
