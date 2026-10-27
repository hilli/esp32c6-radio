package image

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedManifestIsClosedAndExact(t *testing.T) {
	path := "../phy/native/c6-shared-v2-lab1.profile.json"
	p, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSharedV2Manifest(path); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(p), `"partition_start": 3997696`, `"partition_start": 4161536`, 1),
		strings.Replace(string(p), `"physical_status": "NOT_RUN"`, `"physical_status": "PASS"`, 1),
		strings.Replace(string(p), `"version": 1`, `"version": 1, "version": 1`, 1),
		strings.Replace(string(p), `"version": 1`, `"version": 1, "accept_any_geometry": true`, 1),
	} {
		f := filepath.Join(t.TempDir(), "profile.json")
		if err := os.WriteFile(f, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if CheckSharedV2Manifest(f) == nil {
			t.Fatal("wrong/duplicate/open-ended profile manifest accepted")
		}
	}
}
