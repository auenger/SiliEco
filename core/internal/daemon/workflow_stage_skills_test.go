package daemon

import (
	"strings"
	"testing"
)

func TestVerifyRequiredStageSkills(t *testing.T) {
	task := &Task{
		WorkflowStage: &WorkflowStageContextData{RequiredSkills: []string{"silieco-split-feature"}},
		Agent:         &AgentData{Skills: []SkillData{{Name: "silieco-split-feature"}}},
	}
	if err := verifyRequiredStageSkills(task); err != nil {
		t.Fatalf("verifyRequiredStageSkills() error = %v", err)
	}
}

func TestVerifyRequiredStageSkillsFailsClosed(t *testing.T) {
	task := &Task{
		WorkflowStage: &WorkflowStageContextData{RequiredSkills: []string{"silieco-split-feature"}},
		Agent:         &AgentData{},
	}
	err := verifyRequiredStageSkills(task)
	if err == nil || !strings.Contains(err.Error(), "silieco-split-feature") {
		t.Fatalf("verifyRequiredStageSkills() error = %v, want named missing skill", err)
	}
}

func TestWorkflowStageHelpersExposeNextStage(t *testing.T) {
	stage := &WorkflowStageContextData{
		InstanceID: "run-1", InstanceTitle: "Feature SOP", StageID: "stage-1",
		NextStage: &WorkflowNextStageData{StageID: "stage-2", Name: "Develop", StableKey: "develop"},
	}
	if workflowInstanceID(stage) != "run-1" || workflowInstanceTitle(stage) != "Feature SOP" || workflowStageID(stage) != "stage-1" {
		t.Fatalf("current workflow helpers returned the wrong context")
	}
	if nextWorkflowStageID(stage) != "stage-2" || nextWorkflowStageName(stage) != "Develop" || nextWorkflowStageStableKey(stage) != "develop" {
		t.Fatalf("next workflow helpers returned the wrong context")
	}
}
