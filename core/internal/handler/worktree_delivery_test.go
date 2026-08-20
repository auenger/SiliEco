package handler

import "testing"

func TestValidateWorktreeDelivery(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	for _, tc := range []struct {
		name     string
		branch   string
		baseSHA  string
		finalSHA string
		status   string
		wantErr  bool
	}{
		{name: "empty legacy payload"},
		{name: "committed", branch: "silieco/task-1234", baseSHA: sha, finalSHA: sha, status: "committed"},
		{name: "preserved", branch: "silieco/task-1234", baseSHA: sha, status: "preserved"},
		{name: "status without branch", status: "preserved", wantErr: true},
		{name: "unknown status", branch: "silieco/task-1234", status: "merged", wantErr: true},
		{name: "invalid sha", branch: "silieco/task-1234", baseSHA: "not-a-sha", status: "preserved", wantErr: true},
		{name: "newline branch", branch: "silieco/task\nother", status: "preserved", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWorktreeDelivery(tc.branch, tc.baseSHA, tc.finalSHA, tc.status)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
