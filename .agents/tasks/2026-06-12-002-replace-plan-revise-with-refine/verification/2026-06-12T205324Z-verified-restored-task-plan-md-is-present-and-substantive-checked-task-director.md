# Verification: Verified restored task plan.md is present and substantive; checked task director

Result: pass
By: reviewer
Recorded at: 2026-06-12T20:53:24Z

Verified restored task plan.md is present and substantive; checked task directory contents, plan.md content, fixed finding record, and relevant refine/revise references. No new findings.

## Command

$ sed -n '1,220p' .agents/tasks/2026-06-12-002-replace-plan-revise-with-refine/plan.md && rg -n "plan revise|plan refine|revise" internal/app README.md .agents/tasks/2026-06-12-002-replace-plan-revise-with-refine/plan.md
exit: 0

```
...
o revise alias remains. Risks and constraints: do not add ad hoc phrase-based plan rewriting, do not preserve hidden compatibility fallback, keep plan Markdown free-form, and leave true AI-backed durable refinement as a future structural feature.

Write this as concise, editable Markdown rather than filling out a form. Make it specific enough that an implementer and reviewer can act on it without guessing.

Include the substance naturally:
- what outcome the user wants and why this plan is in scope
- repo-relative evidence from docs, code, task state, or prior handoffs
- assumptions, open questions, or decisions still needed
- the implementation shape: files, behavior, and action-sized slices
- validation: focused tests, checks, or acceptance criteria
- risks, trade-offs, and constraints such as permissions, boundaries, or actions Agents must avoid
.agents/tasks/2026-06-12-002-replace-plan-revise-with-refine/plan.md:6:Replace /plan revise with /plan refine without fallback. The code change removes the old revise subcommand from CLI and interactive help, keeps /plan revise rejected, and documents /plan refine as an honest non-mutating command while free-form plans lack a structural rewrite path. Evidence: internal/app/plan.go owns plan subcommand dispatch; internal/app/app.go owns help and loop routing text; internal/app/coordinator.go owns coordinator planning wording; README.md documents public CLI usage; internal/app/app_test.go covers command behavior. Implementation shape already completed by Agents: update command names, reject revise, make refine refuse automatic free-form synthesis without mutating plan.md, and update docs/tests. Validation: reviewer ran GOCACHE=/tmp/agents-gocache-review go test ./internal/app and go test ./..., verified old refine findings, and confirmed no revise alias remains. Risks and constraints: do not add ad hoc phrase-based plan rewriting, do not preserve hidden compatibility fallback, keep plan Markdown free-form, and leave true AI-backed durable refinement as a future structural feature.
README.md:194:agents plan refine "Include docs updates and a focused regression test" # refuses automatic free-form rewrites
README.md:208:`agents plan refine` accepts refinement feedback only when it can be handled
internal/app/plan.go:62:			fmt.Fprintf(errOut, "agents: plan refine: %v\n", err)
internal/app/app_test.go:3223:		if code := Run([]string{"plan", "draft", "Replace plan revise with refine"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
internal/app/app_test.go:3230:		if !strings.Contains(plan.Text, "Replace plan revise with refine") {
internal/app/app_test.go:3276:		if code := Run([]string{"plan", "draft", "Replace plan revise with refine"}, bytes.NewBuffer(nil), &out, &errOut); code != 0 {
internal/app/app_test.go:3283:		code := Run([]string{"plan", "refine", "Rename the command everywhere and reject the old revise subcommand."}, bytes.NewBuffer(nil), &out, &errOut)
internal/app/app_test.go:3285:			t.Fatalf("plan refine code=%d, want 1; stdout=%s stderr=%s", code, out.String(), errOut.String())
internal/app/app_test.go:3292:			t.Fatalf("plan refine should not mutate free-form plan\nbefore=%+v\nafter=%+v", before, after)
internal/app/app_test.go:3295:			t.Fatalf("plan refine persisted feedback instead of refusing synthesis:\n%s", after.Text)
internal/app/app_test.go:3304:		"agents: plan refine: automatic refinement of free-form plans is not supported",
internal/app/app_test.go:3309:			t.Fatalf("plan refine stderr missing %q, got:\n%s", want, gotErr)
internal/app/app_test.go:3321:		return Run([]string{"plan", "revise", "old spelling"}, bytes.NewBuffer(nil), &out, &errOut)
internal/app/app_test.go:3324:		t.Fatalf("plan revise code=%d, want 2; stdout=%s stderr=%s", code, out.String(), errOut.String())
internal/app/app_test.go:3326:	if !strings.Contains(errOut.String(), "unknown plan command: revise") {
internal/app/app_test.go:3327:		t.Fatalf("unexpected stderr for revise:\n%s", errOut.String())
```
