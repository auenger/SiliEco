package skillbundle

import "testing"

func revisionFixture() RevisionSpec {
	return RevisionSpec{Skill: Skill{ID: "skill-1", Source: SourceWorkspace, Name: "builder", Content: "# Build", Files: []File{{Path: "scripts/build.sh", Content: "echo build"}}}, CompatibleContracts: []string{"result/v2", "result/v1"}, ToolDependencies: []ToolDependency{{Name: "compiler", Version: "1.2.3"}}}
}
func TestRevisionManifestPinsContentContractsAndTools(t *testing.T) {
	original := revisionFixture()
	base, err := BuildRevisionManifest(original)
	if err != nil {
		t.Fatal(err)
	}
	if base.BundleHash != BuildManifest(original.Skill).Hash {
		t.Fatal("existing bundle identity changed")
	}
	for _, mutate := range []func(*RevisionSpec){func(s *RevisionSpec) { s.Skill.Content += " updated" }, func(s *RevisionSpec) { s.Skill.Files[0].Content += " updated" }, func(s *RevisionSpec) { s.CompatibleContracts = []string{"result/v3"} }, func(s *RevisionSpec) { s.ToolDependencies[0].Version = "2.0.0" }} {
		changed := revisionFixture()
		mutate(&changed)
		next, err := BuildRevisionManifest(changed)
		if err != nil {
			t.Fatal(err)
		}
		if next.Hash == base.Hash {
			t.Fatal("revision ignored changed content or environment")
		}
	}
	original.CompatibleContracts = []string{"result/v1", "result/v2"}
	same, err := BuildRevisionManifest(original)
	if err != nil || same.Hash != base.Hash {
		t.Fatalf("contract set order changed revision: %v", err)
	}
}
func TestRevisionManifestRejectsUnsafeOrAmbiguousFiles(t *testing.T) {
	for _, filePath := range []string{"", ".", "C:/script", "/tmp/script", "../script", "scripts/../script", "scripts\\script", "SKILL.md", "skill.md", "scripts/build.sh", "SCRIPTS/build.sh", "a\x00b"} {
		spec := revisionFixture()
		spec.Skill.Files = append(spec.Skill.Files, File{Path: filePath, Content: "x"})
		if _, err := BuildRevisionManifest(spec); err == nil {
			t.Errorf("accepted path %q", filePath)
		}
	}
}
func TestRevisionManifestOwnsMetadataCopies(t *testing.T) {
	spec := revisionFixture()
	manifest, err := BuildRevisionManifest(spec)
	if err != nil {
		t.Fatal(err)
	}
	if spec.CompatibleContracts[0] != "result/v2" {
		t.Fatal("mutated caller's contract order")
	}
	spec.ToolDependencies[0].Version = "new"
	spec.CompatibleContracts[0] = "new"
	if manifest.ToolDependencies[0].Version != "1.2.3" || manifest.CompatibleContracts[1] != "result/v2" {
		t.Fatal("manifest aliases mutable caller metadata")
	}
}
func TestRevisionManifestRequiresPinnedDependencyIdentity(t *testing.T) {
	for _, tools := range [][]ToolDependency{{{Name: "compiler", Version: ""}}, {{Name: "compiler", Version: "1"}, {Name: "compiler", Version: "2"}}} {
		spec := revisionFixture()
		spec.ToolDependencies = tools
		if _, err := BuildRevisionManifest(spec); err == nil {
			t.Fatal("accepted ambiguous dependencies")
		}
	}
}

func TestRevisionManifestRejectsIncompleteIdentityAndContracts(t *testing.T) {
	for _, mutate := range []func(*RevisionSpec){func(s *RevisionSpec) { s.Skill.ID = "" }, func(s *RevisionSpec) { s.Skill.Source = "unknown" }, func(s *RevisionSpec) { s.Skill.Content = " " }, func(s *RevisionSpec) { s.CompatibleContracts = []string{"result/v1", "result/v1"} }, func(s *RevisionSpec) { s.CompatibleContracts = []string{" result/v1 "} }} {
		spec := revisionFixture()
		mutate(&spec)
		if _, err := BuildRevisionManifest(spec); err == nil {
			t.Fatal("accepted incomplete revision identity")
		}
	}
}
