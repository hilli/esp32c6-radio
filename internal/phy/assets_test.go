package phy

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestNativeAssetIntegrity(t *testing.T) {
	data, err := os.ReadFile("native/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		IDF   string `json:"esp_idf_revision"`
		PHY   string `json:"esp_phy_lib_revision"`
		Files []struct {
			Name, URL, SHA256 string
		}
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 7 || len(manifest.IDF) != 40 || len(manifest.PHY) != 40 {
		t.Fatal("missing pinned native dependencies")
	}
	for _, file := range manifest.Files {
		t.Run(file.Name, func(t *testing.T) {
			if !strings.HasPrefix(file.URL, "https://raw.githubusercontent.com/") {
				t.Fatal("unexpected provenance host")
			}
			data, err := os.ReadFile("native/" + file.Name)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprintf("%x", sha256.Sum256(data)) != file.SHA256 {
				t.Fatal("asset hash differs from pinned manifest")
			}
			if strings.HasSuffix(file.Name, ".a") && !bytes.HasPrefix(data, []byte("!<arch>\n")) {
				t.Fatal("not a real archive")
			}
		})
	}
}
