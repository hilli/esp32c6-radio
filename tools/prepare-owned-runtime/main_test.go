package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	parent := t.TempDir()
	source, output := filepath.Join(parent, "stock"), filepath.Join(parent, "private")
	for _, name := range []string{"bin", "lib", "targets"} {
		if err := os.MkdirAll(filepath.Join(source, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	guard := "src/runtime/interrupt/guard.go"
	task := "src/internal/task/task.go"
	write(t, filepath.Join(source, guard), "package interrupt\nvar old int\n")
	write(t, filepath.Join(source, task), "package task\n")
	write(t, filepath.Join(source, "src/device/register.go"), "package device\n")
	before, err := digest(filepath.Join(source, guard))
	if err != nil {
		t.Fatal(err)
	}
	taskHash, err := digest(filepath.Join(source, task))
	if err != nil {
		t.Fatal(err)
	}
	overlayGuard := filepath.Join(parent, "new-guard")
	overlayTask := filepath.Join(parent, "new-task")
	write(t, overlayGuard, "package interrupt\nvar owned int\n")
	write(t, overlayTask, "package task\nvar owned int\n")
	guardHash, _ := digest(overlayGuard)
	newTaskHash, _ := digest(overlayTask)
	patch := filepath.Join(parent, "tinygo-0.44-owned.patch")
	write(t, patch, `diff --git a/src/runtime/interrupt/guard.go b/src/runtime/interrupt/guard.go
--- a/src/runtime/interrupt/guard.go
+++ b/src/runtime/interrupt/guard.go
@@ -1,2 +1,2 @@
 package interrupt
-var old int
+var owned int
diff --git a/src/internal/task/owned.go b/src/internal/task/owned.go
new file mode 100644
--- /dev/null
+++ b/src/internal/task/owned.go
@@ -0,0 +1,2 @@
+package task
+var owned int
`)
	patchHash, _ := digest(patch)
	pin := manifest{TinyGo: "0.44.0", Patch: patchHash,
		Sources: map[string]string{guard: before, task: taskHash},
		Changes: map[string]change{
			guard:                        {Original: &before, Overlay: guardHash},
			"src/internal/task/owned.go": {Overlay: newTaskHash},
		}}
	data, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(parent, "pinned.json")
	write(t, pinPath, string(data))
	return source, output, pinPath, before
}

func TestPreparePrivateSourceWithoutTouchingStock(t *testing.T) {
	source, output, pin, before := fixture(t)
	// Deliberately put the output under a Git repository. The local apply must
	// not discover/use that repository's index or outside working directory.
	cmd := exec.Command("git", "init", "--quiet", filepath.Dir(output))
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fixture: %v: %s", err, data)
	}
	if err := prepare(source, output, pin); err != nil {
		t.Fatal(err)
	}
	if err := check(filepath.Join(source, "src/runtime/interrupt/guard.go"), before); err != nil {
		t.Fatalf("stock source changed: %v", err)
	}
	for _, name := range []string{"src/runtime/interrupt/guard.go", "src/internal/task/owned.go"} {
		data, err := os.ReadFile(filepath.Join(output, name))
		if err != nil || !strings.Contains(string(data), "var owned int") {
			t.Fatalf("overlay %s: %v, %q", name, err, data)
		}
	}
	for _, name := range []string{"bin", "lib", "targets", "src/device"} {
		info, err := os.Lstat(filepath.Join(output, name))
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("stock read-only-use link %s: %v", name, err)
		}
	}
	if info, err := os.Lstat(filepath.Join(output, "src/runtime")); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("patch destination aliases stock runtime")
	}
}

func TestRefuseSourceMismatchBeforeCreatingOutput(t *testing.T) {
	source, output, pin, _ := fixture(t)
	write(t, filepath.Join(source, "src/internal/task/task.go"), "changed source\n")
	if err := prepare(source, output, pin); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("source mismatch accepted: %v", err)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("output created before all source pins passed")
	}
}

func TestRefuseExistingOrStockOutput(t *testing.T) {
	source, output, pin, _ := fixture(t)
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(output, "sentinel"), "keep")
	if err := prepare(source, output, pin); err == nil {
		t.Fatal("existing output accepted")
	}
	if data, _ := os.ReadFile(filepath.Join(output, "sentinel")); string(data) != "keep" {
		t.Fatal("existing output damaged")
	}
	if err := prepare(source, filepath.Join(source, "new"), pin); err == nil {
		t.Fatal("installed-source output accepted")
	}
	alias := filepath.Join(filepath.Dir(output), "stock-alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	if err := prepare(source, filepath.Join(alias, "new"), pin); err == nil {
		t.Fatal("source alias bypassed output guard")
	}
}

func TestRefuseAlteredPatchAndExistingNewPath(t *testing.T) {
	t.Run("patch", func(t *testing.T) {
		source, output, pin, _ := fixture(t)
		write(t, filepath.Join(filepath.Dir(pin), "tinygo-0.44-owned.patch"), "altered")
		if err := prepare(source, output, pin); err == nil {
			t.Fatal("modified patch accepted")
		}
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			t.Fatal("output created with unchecked patch")
		}
	})
	t.Run("new-path", func(t *testing.T) {
		source, output, pin, _ := fixture(t)
		write(t, filepath.Join(source, "src/internal/task/owned.go"), "unexpected")
		if err := prepare(source, output, pin); err == nil {
			t.Fatal("preexisting overlay path accepted")
		}
	})
}
