package engine

import "testing"

func TestParseTaskCommand(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantOK    bool
		wantTitle string
		wantDesc  string
	}{
		{"canonical task title", "/task Fix the login bug", true, "Fix the login bug", ""},
		{"canonical task + description", "/task Fix login\nIt 500s on submit", true, "Fix login", "It 500s on submit"},
		{"canonical bare task", "/task", true, "", ""},
		{"title only", "/task Fix the login bug", true, "Fix the login bug", ""},
		{"title + description", "/task Fix login\nIt 500s on submit\nsince Tuesday", true, "Fix login", "It 500s on submit\nsince Tuesday"},
		{"bare command", "/task", true, "", ""},
		{"bare command with trailing space", "/task   ", true, "", ""},
		{"leading blank lines", "\n\n/task Title", true, "Title", ""},
		{"tab separator", "/task\tTabbed", true, "Tabbed", ""},
		{"not a token", "/issuetracker do thing", false, "", ""},
		{"prefix mid-sentence", "hey /task not a command", false, "", ""},
		{"case sensitive", "/Task Title", false, "", ""},
		{"empty", "", false, "", ""},
		{"only whitespace", "   \n  ", false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok := ParseTaskCommand(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				if cmd != nil {
					t.Errorf("cmd = %+v, want nil", cmd)
				}
				return
			}
			if cmd.Title != tc.wantTitle || cmd.Description != tc.wantDesc {
				t.Errorf("cmd = {%q, %q}, want {%q, %q}", cmd.Title, cmd.Description, tc.wantTitle, tc.wantDesc)
			}
		})
	}
}

func TestTitleFromPreviousMessage(t *testing.T) {
	if got := titleFromPreviousMessage("/task Real title"); got != "Real title" {
		t.Errorf("prev /task should strip prefix: %q", got)
	}
	if got := titleFromPreviousMessage("\n  first line\nsecond"); got != "first line" {
		t.Errorf("prev plain should take first non-empty line: %q", got)
	}
	if got := titleFromPreviousMessage("   \n  "); got != "" {
		t.Errorf("blank prev → empty: %q", got)
	}
}
