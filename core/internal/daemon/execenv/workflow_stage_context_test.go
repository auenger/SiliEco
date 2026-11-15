package execenv

import (
	"strings"
	"testing"
)

func TestRenderTaskContextIncludesCurrentAndNextSOPStage(t *testing.T) {
	out := renderTaskContext("codex", RunContextForEnv{
		TaskID:                     "task-1",
		WorkflowInstanceID:         "run-1",
		WorkflowInstanceTitle:      "Payment SOP",
		WorkflowStageID:            "stage-current",
		WorkflowStageName:          "Split",
		WorkflowStageStableKey:     "split",
		NextWorkflowStageID:        "stage-next",
		NextWorkflowStageName:      "Develop",
		NextWorkflowStageStableKey: "develop",
		AvailableWorkflowStages: []WorkflowStageForEnv{
			{ID: "stage-current", Name: "Split", StableKey: "split", Position: 1},
			{ID: "stage-mvp", Name: "Prototype or MVP", StableKey: "prototype", Position: 2},
			{ID: "stage-next", Name: "Develop", StableKey: "develop", Position: 3},
		},
		RequiredSkillNames: []string{"silieco-split-feature"},
	})
	for _, want := range []string{
		"Payment SOP", "run-1", "stage-current", "Split", "stage-next", "Develop",
		"stage-mvp", "Prototype or MVP", "--workflow-instance run-1 --workflow-stage <stage-id>",
		"Backlog or human assignment", "silieco-split-feature",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("task context missing %q:\n%s", want, out)
		}
	}
}
