//go:build linux

package main

import "testing"

// bluefinStatus is a trimmed but representative `rpm-ostree status --json`
// payload for an image-mode host (Bluefin LTS), with a booted deployment and a
// pending/staged deployment. Keys are hyphenated, matching rpm-ostree.
const bluefinStatus = `{
  "deployments": [
    {
      "id": "default-1111111111111111111111111111111111111111111111111111111111111111.0",
      "osname": "default",
      "checksum": "1111111111111111111111111111111111111111111111111111111111111111",
      "version": "42.20260601.0",
      "timestamp": 1762837557,
      "booted": false,
      "staged": true,
      "container-image-reference": "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:lts",
      "packages": ["htop", "vim-enhanced"]
    },
    {
      "id": "default-2222222222222222222222222222222222222222222222222222222222222222.0",
      "osname": "default",
      "checksum": "2222222222222222222222222222222222222222222222222222222222222222",
      "version": "42.20260515.0",
      "timestamp": 1762737557,
      "booted": true,
      "staged": false,
      "container-image-reference": "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:lts",
      "packages": []
    }
  ]
}`

func TestParseStatus_TwoDeployments(t *testing.T) {
	rows, err := parseStatus([]byte(bluefinStatus))
	if err != nil {
		t.Fatalf("parseStatus error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	staged, booted := rows[0], rows[1]

	if staged["error"] != "" || booted["error"] != "" {
		t.Fatalf("unexpected error column: staged=%q booted=%q", staged["error"], booted["error"])
	}

	// Staged (first) deployment.
	if staged["staged"] != "1" || staged["booted"] != "0" {
		t.Errorf("staged row: expected staged=1 booted=0, got staged=%q booted=%q", staged["staged"], staged["booted"])
	}
	if staged["layered_packages"] != `["htop","vim-enhanced"]` {
		t.Errorf("staged row layered_packages: got %q", staged["layered_packages"])
	}

	// Booted (second) deployment.
	if booted["booted"] != "1" || booted["staged"] != "0" {
		t.Errorf("booted row: expected booted=1 staged=0, got booted=%q staged=%q", booted["booted"], booted["staged"])
	}
	if booted["container_image_reference"] != "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:lts" {
		t.Errorf("booted row container_image_reference: got %q", booted["container_image_reference"])
	}
	// No layered packages on the booted image: must be a valid empty JSON array.
	if booted["layered_packages"] != "[]" {
		t.Errorf("booted row layered_packages: expected [], got %q", booted["layered_packages"])
	}
	if booted["version"] != "42.20260515.0" {
		t.Errorf("booted row version: got %q", booted["version"])
	}
	if booted["checksum"] != "2222222222222222222222222222222222222222222222222222222222222222" {
		t.Errorf("booted row checksum: got %q", booted["checksum"])
	}
}

func TestParseStatus_NoDeployments(t *testing.T) {
	rows, err := parseStatus([]byte(`{"deployments": []}`))
	if err != nil {
		t.Fatalf("parseStatus error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestParseStatus_InvalidJSON(t *testing.T) {
	rows, err := parseStatus([]byte("not json"))
	if err != nil {
		t.Fatalf("parseStatus should not hard-error on bad JSON: %v", err)
	}
	if len(rows) != 1 || rows[0]["error"] == "" {
		t.Errorf("expected a single error row, got %v", rows)
	}
}

func TestMarshalPackages(t *testing.T) {
	if got := marshalPackages(nil); got != "[]" {
		t.Errorf("nil packages: expected [], got %q", got)
	}
	if got := marshalPackages([]string{}); got != "[]" {
		t.Errorf("empty packages: expected [], got %q", got)
	}
	if got := marshalPackages([]string{"a", "b"}); got != `["a","b"]` {
		t.Errorf("packages: got %q", got)
	}
}

func TestColumns(t *testing.T) {
	want := []string{
		"id", "version", "checksum", "booted", "staged",
		"container_image_reference", "layered_packages", "error",
	}
	cols := columns()
	if len(cols) != len(want) {
		t.Fatalf("expected %d columns, got %d", len(want), len(cols))
	}
	for i, name := range want {
		if cols[i].Name != name {
			t.Errorf("column %d: expected %q, got %q", i, name, cols[i].Name)
		}
	}
}
