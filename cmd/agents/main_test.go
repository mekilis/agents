package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunMainPrintsVersionMetadata(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "test-version", "test-commit", "2026-06-12"
	t.Cleanup(func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	})

	var out, errOut bytes.Buffer
	code := runMain([]string{"version"}, bytes.NewBuffer(nil), &out, &errOut)
	if code != 0 {
		t.Fatalf("version code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"agents test-version", "test-commit", "2026-06-12"} {
		if !strings.Contains(got, want) {
			t.Fatalf("version output missing %q:\n%s", want, got)
		}
	}
}
