package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestMigrationFilesHaveMatchingDirections(t *testing.T) {
	files := migrationFilesForLint(t, "*.sql")
	directions := map[string]map[string]bool{}
	for _, file := range files {
		stem, direction, ok := splitMigrationFilename(filepath.Base(file))
		if !ok {
			t.Fatalf("invalid migration filename %s", file)
		}
		if directions[stem] == nil {
			directions[stem] = map[string]bool{}
		}
		directions[stem][direction] = true
	}
	for stem, dirs := range directions {
		if !dirs["up"] || !dirs["down"] {
			t.Errorf("migration %s requires both directions", stem)
		}
	}
}
func TestMigrationPrefixesAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, file := range migrationFilesForLint(t, "*.up.sql") {
		stem := ExtractVersion(file)
		prefix, _, ok := strings.Cut(stem, "_")
		if !ok || !regexp.MustCompile(`^[0-9]{4}$`).MatchString(prefix) {
			t.Errorf("invalid numeric prefix: %s", stem)
		}
		if previous, exists := seen[prefix]; exists {
			t.Errorf("duplicate prefix %s: %s, %s", prefix, previous, stem)
		}
		seen[prefix] = stem
	}
}
func TestBaselineConstraintsAndConcurrentIndexes(t *testing.T) {
	indexPattern := regexp.MustCompile(`(?i)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+`)
	concurrentPattern := regexp.MustCompile(`(?i)^CREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+`)
	foreignKey := regexp.MustCompile(`(?i)\bFOREIGN\s+KEY\b|\bREFERENCES\s+`)
	for _, file := range migrationFilesForLint(t, "*.up.sql") {
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sql := string(contents)
		if foreignKey.MatchString(sql) {
			t.Errorf("%s introduces a database foreign key", filepath.Base(file))
		}
		if indexPattern.MatchString(sql) {
			trimmed := strings.TrimSpace(sql)
			if !concurrentPattern.MatchString(trimmed) || strings.Count(trimmed, ";") != 1 {
				t.Errorf("%s must contain one concurrent index statement", filepath.Base(file))
			}
		}
	}
	if _, err := os.Stat(filepath.Join(realMigrationsDir(t), BaselineVersion+".up.sql")); err != nil {
		t.Fatal(err)
	}
}
func migrationFilesForLint(t *testing.T, pattern string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(realMigrationsDir(t), pattern))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no migrations match %s", pattern)
	}
	sort.Strings(files)
	return files
}
func realMigrationsDir(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migration path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(self), "..", "..", "migrations"))
}
func splitMigrationFilename(name string) (stem, direction string, ok bool) {
	for _, d := range []string{"up", "down"} {
		suffix := fmt.Sprintf(".%s.sql", d)
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix), d, true
		}
	}
	return "", "", false
}
