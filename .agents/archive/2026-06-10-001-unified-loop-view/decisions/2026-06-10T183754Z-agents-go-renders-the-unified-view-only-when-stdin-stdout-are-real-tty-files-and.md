# Decision: agents go renders the unified view only when stdin/stdout are real TTY files and

Status: proposed
By: implementer

agents go renders the unified view only when stdin/stdout are real TTY files and --plain is absent; q is confirm-then-stop and p pauses, both via loopControl checked between role runs so the current run always completes; Ctrl-C forwards os.Interrupt to reuse run interruption
