# Decision: M1 entry point is agents go: one instruction argument; 'title: instruction' spli

Status: proposed
By: implementer

M1 entry point is agents go: one instruction argument; 'title: instruction' splits on the first colon-space (bare colons like URLs do not split); plain text is both title and scope; supports --ref and --max-iterations; reuses the managed loop via shared runLoopToStop
