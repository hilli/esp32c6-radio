package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilli/esp32c6-radio/lab"
)

func TestMissingEvidencePersistsNotRunAndReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	log := filepath.Join(dir, "capture.log")
	out := filepath.Join(dir, "evidence.json")
	if err := save(manifest, lab.Manifest{Version: 1, RunID: "prepared", FirmwareID: "image", Probe: "storage"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := collect([]string{"-manifest", manifest, "-input", log, "-out", out}); err == nil {
		t.Fatal("missing device/capture reported success")
	}
	p, err := os.ReadFile(out)
	if err != nil || len(p) == 0 {
		t.Fatal("NOT_RUN evidence was lost", err)
	}
}
func TestUnknownProbeCannotTouchADeviceOrInvokeBuild(t *testing.T) {
	if err := build([]string{"-probe", "arbitrary-executable"}); err == nil {
		t.Fatal("unknown probe accepted")
	}
}
func TestCommandsRequireExplicitScope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	m := lab.Manifest{Version: 1, RunID: "run", FirmwareID: "firmware", Board: "prepared synthetic board", ExpectedEUI: "600102fffe030405"}
	if err := save(path, m); err != nil {
		t.Fatal(err)
	}
	if err := commands([]string{"-manifest", path, "-boot", "test", "-operation", "format"}); err == nil {
		t.Fatal("destructive command emitted without scope")
	}
	if err := commands([]string{"-manifest", path, "-boot", "test", "-operation", "run", "-probe", "rf", "-channel", "15", "-local", "4660", "-peer", "22136", "-approve-scope", m.ExpectedEUI + ":rf:" + m.RunID}); err == nil {
		t.Fatal("RF command silently selected a default PAN")
	}
	if err := commands([]string{"-manifest", path, "-boot", "test", "-operation", "start", "-probe", "thread", "-approve-scope", m.ExpectedEUI + ":thread-lab:" + m.RunID}); err == nil {
		t.Fatal("Thread command emitted without actual storage/source evidence")
	}
}

func TestPhysicalPrerequisitesAndPrivateOutputUsingSyntheticHostFixtures(t *testing.T) {
	dir := t.TempDir()
	manifest, storage, entropy := filepath.Join(dir, "manifest.json"), filepath.Join(dir, "storage.json"), filepath.Join(dir, "entropy.json")
	review, dataset, out := filepath.Join(dir, "review.json"), filepath.Join(dir, "private.txt"), filepath.Join(dir, "command.json")
	eui := "600102fffe030405"
	m := lab.Manifest{Version: 1, RunID: "synthetic-host-test", FirmwareID: "image", Board: "synthetic fixture only", ExpectedEUI: eui, NativeRevision: "synthetic-revision"}
	for path, v := range map[string]any{
		manifest: m,
		storage:  lab.Report{Version: 1, Origin: "physical", DeviceEUI: eui, FirmwareID: "storage-fixture", Checks: map[string]lab.Status{"identity": lab.Pass, "storage_safety": lab.Pass, "storage_recovery": lab.Pass}},
		entropy:  lab.Report{Version: 1, Origin: "synthetic", DeviceEUI: eui, FirmwareID: "entropy-fixture", Checks: map[string]lab.Status{"identity": lab.Pass, "entropy_health": lab.Pass}},
		review:   map[string]any{"device_eui64": eui, "native_revision": m.NativeRevision, "source_artifacts": []string{"synthetic-a", "synthetic-b"}, "review": strings.Repeat("synthetic host command test; no hardware claim. ", 5)},
	} {
		if err := save(path, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(dataset, []byte("synthetic-placeholder-not-a-network-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-manifest", manifest, "-boot", "fixture", "-probe", "thread", "-operation", "start", "-approve-scope", eui + ":thread-lab:" + m.RunID, "-storage-evidence", storage, "-entropy-evidence", entropy, "-source-review", review, "-dataset-file", dataset}
	if err := commands(args); err == nil {
		t.Fatal("synthetic prerequisite accepted")
	}
	if err := save(entropy, lab.Report{Version: 1, Origin: "physical", DeviceEUI: eui, FirmwareID: "synthetic-entropy-fixture", Checks: map[string]lab.Status{"identity": lab.Pass, "entropy_health": lab.Pass}}); err != nil {
		t.Fatal(err)
	}
	if err := commands(args); err == nil {
		t.Fatal("private dataset was allowed on stdout")
	}
	if err := os.WriteFile(out, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := commands(append(args, "-out-command", out)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("private output permissions", err)
	}
	p, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var c lab.Command
	if err := json.Unmarshal(p, &c); err != nil || c.DatasetHex == "" || len(c.EntropyReviewArtifact) != 64 {
		t.Fatal("private command missing required artifacts", err)
	}
}
