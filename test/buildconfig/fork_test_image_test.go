/*
Copyright 2026 pgcopydb-operator contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package buildconfig

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestForkTestImageSourceGate(t *testing.T) {
	job := mustParse(t, "../../.github/workflows/fork-test-image.yml").Jobs["image"]
	source := slices.IndexFunc(job.Steps, func(step workflowStep) bool { return step.ID == "source" })
	checkout := slices.IndexFunc(job.Steps, func(step workflowStep) bool { return step.Uses == "actions/checkout@v7" })
	if source < 0 || checkout <= source || job.Steps[source].If != "" {
		t.Fatal("source approval must run unconditionally before checkout")
	}

	sha := strings.Repeat("a", 40)
	const runID = "123"
	for _, tt := range []struct {
		name        string
		source      string
		ciRun       string
		missing     int
		cliStatus   int
		wantStatus  int
		wantMessage string
	}{
		{name: "approved commit", source: sha, ciRun: runID},
		{name: "branch refused", source: "main", wantStatus: 1, wantMessage: "full lowercase commit SHA"},
		{name: "no successful CI", source: sha, wantStatus: 1, wantMessage: "No successful fork CI"},
		{name: "required job skipped", source: sha, ciRun: runID, missing: 1, wantStatus: 1, wantMessage: "must pass lint"},
		{name: "API failure", source: sha, cliStatus: 17, wantStatus: 17},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "outputs")
			calls := filepath.Join(dir, "calls")
			cmd := exec.Command("bash", "-eo", "pipefail", "-c", `
gh() {
  echo called >> "$MOCK_CALLS"
  if [ "$MOCK_STATUS" != 0 ]; then return "$MOCK_STATUS"; fi
  case "$2" in
    list) printf '%s\n' "$MOCK_RUN" ;;
    view) printf '%s\n' "$MOCK_MISSING" ;;
    *) return 99 ;;
  esac
}
`+job.Steps[source].Run)
			cmd.Env = append(os.Environ(),
				"SOURCE_SHA="+tt.source, "MOCK_RUN="+tt.ciRun, "MOCK_CALLS="+calls,
				fmt.Sprintf("MOCK_MISSING=%d", tt.missing), fmt.Sprintf("MOCK_STATUS=%d", tt.cliStatus),
				"GITHUB_OUTPUT="+output, "GITHUB_STEP_SUMMARY="+filepath.Join(dir, "summary"),
				"GITHUB_REPOSITORY=example/operator", "GITHUB_RUN_ID=789", "GITHUB_RUN_ATTEMPT=1")
			out, err := cmd.CombinedOutput()
			if cmd.ProcessState == nil {
				t.Fatalf("run source gate: %v", err)
			}
			if got := cmd.ProcessState.ExitCode(); got != tt.wantStatus {
				t.Fatalf("exit = %d, want %d: %v\n%s", got, tt.wantStatus, err, out)
			}
			if !strings.Contains(string(out), tt.wantMessage) {
				t.Errorf("missing error %q:\n%s", tt.wantMessage, out)
			}
			if tt.wantStatus != 0 {
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatalf("refused source must not emit build outputs: %v", err)
				}
				if tt.source != sha {
					if _, err := os.Stat(calls); !os.IsNotExist(err) {
						t.Fatalf("invalid SHA reached the API: %v", err)
					}
				}
				return
			}
			got := read(t, output)
			want := "sha=" + sha + "\nversion=test-" + sha[:12] + "-789-1\n"
			if got != want {
				t.Errorf("build outputs = %q, want %q", got, want)
			}
		})
	}
}
