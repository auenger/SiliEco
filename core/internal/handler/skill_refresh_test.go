package handler

import "testing"

func TestSkillRefreshFileDiff(t *testing.T) {
	current := &importedSkill{content: "old", files: []importedFile{{path: "same.txt", content: "same"}, {path: "removed.txt", content: "old"}}}
	source := &importedSkill{content: "new", files: []importedFile{{path: "same.txt", content: "same"}, {path: "added.txt", content: "new"}}}
	diffs := skillRefreshFileDiff(current, source)
	statuses := make(map[string]string, len(diffs))
	for _, diff := range diffs {
		statuses[diff.Path] = diff.Status
	}
	for path, want := range map[string]string{"SKILL.md": "modified", "same.txt": "unchanged", "removed.txt": "deleted", "added.txt": "added"} {
		if got := statuses[path]; got != want {
			t.Errorf("%s status = %q, want %q", path, got, want)
		}
	}
}
