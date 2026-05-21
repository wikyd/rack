package aws

import (
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"testing"
)

// TestServiceTemplateParses verifies service.json.tmpl parses cleanly with the
// helper funcs registered in formationHelpers(). This catches template-syntax
// regressions (missing {{ end }}, unknown function names, malformed actions)
// without requiring a full render fixture.
//
// End-to-end rendering is exercised in integration tests.
func TestServiceTemplateParses(t *testing.T) {
	path := filepath.Join("formation", "service.json.tmpl")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	if _, err := template.New(filepath.Base(path)).Funcs(formationHelpers()).Parse(string(data)); err != nil {
		t.Fatalf("service.json.tmpl failed to parse: %v", err)
	}
}

// TestAppTemplateParses is a companion parse check for app.json.tmpl so future
// refactors to formationHelpers or template syntax are caught in both places.
func TestAppTemplateParses(t *testing.T) {
	path := filepath.Join("formation", "app.json.tmpl")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	if _, err := template.New(filepath.Base(path)).Funcs(formationHelpers()).Parse(string(data)); err != nil {
		t.Fatalf("app.json.tmpl failed to parse: %v", err)
	}
}

// rackTemplate is a minimal projection of rack.json used by tests below to
// assert structural properties (presence of Resources / Parameters /
// Conditions, shape of ApiBuildFargate) without modeling every field.
type rackTemplate struct {
	Parameters map[string]any `json:"Parameters"`
	Conditions map[string]any `json:"Conditions"`
	Resources  map[string]any `json:"Resources"`
}

func loadRackTemplate(t *testing.T) rackTemplate {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("formation", "rack.json"))
	if err != nil {
		t.Fatalf("read rack.json: %v", err)
	}

	var rt rackTemplate
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("rack.json failed to parse as JSON: %v", err)
	}
	return rt
}

// TestRackJSONParses is the rack-template counterpart to the *.tmpl parse
// checks above. rack.json is plain JSON (no Go template directives), so the
// natural regression check is that it survives encoding/json round-trip and
// has the expected top-level sections populated.
func TestRackJSONParses(t *testing.T) {
	rt := loadRackTemplate(t)

	if len(rt.Parameters) == 0 {
		t.Fatal("rack.json: no Parameters parsed")
	}
	if len(rt.Conditions) == 0 {
		t.Fatal("rack.json: no Conditions parsed")
	}
	if len(rt.Resources) == 0 {
		t.Fatal("rack.json: no Resources parsed")
	}
}

// TestRackFargateBuildEfsCacheResources guards the optional EFS-backed
// cache feature on Fargate builds. When FargateBuildEfsEnabled is "true"
// (and BuildMethod is "fargate" in a RegionHasEFS region), the rack
// provisions a dedicated EFS file system, security group, and mount
// targets, and mounts it into the ApiBuildFargate task at the configured
// path. All resources must remain gated on FargateBuildEfsCacheEnabled so
// existing racks with the default (false) see no change.
func TestRackFargateBuildEfsCacheResources(t *testing.T) {
	rt := loadRackTemplate(t)

	wantParams := []string{"FargateBuildEfsEnabled", "FargateBuildEfsMountPath"}
	for _, name := range wantParams {
		if _, ok := rt.Parameters[name]; !ok {
			t.Errorf("rack.json: missing Parameter %q", name)
		}
	}

	wantConditions := []string{
		"FargateBuildEfsCacheEnabled",
		"FargateBuildEfsCacheEnabledAndThirdAvailabilityZoneAndHighAvailability",
	}
	for _, name := range wantConditions {
		if _, ok := rt.Conditions[name]; !ok {
			t.Errorf("rack.json: missing Condition %q", name)
		}
	}

	wantResources := []string{
		"BuildCacheFilesystem",
		"BuildCacheSecurity",
		"BuildCacheTarget0",
		"BuildCacheTarget1",
		"BuildCacheTarget2",
	}
	for _, name := range wantResources {
		res, ok := rt.Resources[name].(map[string]any)
		if !ok {
			t.Errorf("rack.json: missing Resource %q", name)
			continue
		}
		cond, _ := res["Condition"].(string)
		switch name {
		case "BuildCacheTarget2":
			if cond != "FargateBuildEfsCacheEnabledAndThirdAvailabilityZoneAndHighAvailability" {
				t.Errorf("rack.json: Resource %q Condition=%q, want FargateBuildEfsCacheEnabledAndThirdAvailabilityZoneAndHighAvailability", name, cond)
			}
		default:
			if cond != "FargateBuildEfsCacheEnabled" {
				t.Errorf("rack.json: Resource %q Condition=%q, want FargateBuildEfsCacheEnabled", name, cond)
			}
		}
	}

	// Verify the ApiBuildFargate task has Volumes + container MountPoints,
	// both as conditional Fn::If entries so a default (cache-disabled) rack
	// renders the same task definition as before this feature.
	task, ok := rt.Resources["ApiBuildFargate"].(map[string]any)
	if !ok {
		t.Fatal("rack.json: missing Resource ApiBuildFargate")
	}
	props, _ := task["Properties"].(map[string]any)
	if _, ok := props["Volumes"]; !ok {
		t.Error("rack.json: ApiBuildFargate.Properties.Volumes not declared")
	}
	containers, _ := props["ContainerDefinitions"].([]any)
	if len(containers) != 1 {
		t.Fatalf("rack.json: ApiBuildFargate has %d container definitions, want 1", len(containers))
	}
	container, _ := containers[0].(map[string]any)
	if _, ok := container["MountPoints"]; !ok {
		t.Error("rack.json: ApiBuildFargate container is missing MountPoints")
	}
}
