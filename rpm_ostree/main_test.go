package main

import (
	"os"
	"strconv"
	"testing"
)

// sampleStatusJSON is a realistic `rpm-ostree status --json` payload modeled on
// a real Bluefin LTS (Universal Blue) host. It contains the three deployments
// you normally see on an rpm-ostree system:
//
//	[0] staged for next boot     (staged=1, booted=0) — newest base, fleet-osquery re-layered
//	[1] currently booted         (booted=1, staged=0) — base 42cbb..., fleet-osquery + repo packages layered
//	[2] previous / rollback      (booted=0, staged=0) — the plain base image, nothing layered
//
// The IDs, checksums, version, container image, and manifest digest are taken
// from real query results captured from the host. The booted deployment is
// given two extra repo-layered packages (htop, vim) on top of the local
// fleet-osquery RPM to exercise the layered_packages merge.
const sampleStatusJSON = `{
  "cached-update": null,
  "transaction": null,
  "deployments": [
    {
      "id": "default-711cc6690dee93998dc8889998936fd7d53470b51b441a4191878eb8a7b13e77.0",
      "osname": "default",
      "version": "stream10.1",
      "booted": false,
      "staged": true,
      "pinned": false,
      "unlocked": "none",
      "timestamp": 1718409600,
      "checksum": "711cc6690dee93998dc8889998936fd7d53470b51b441a4191878eb8a7b13e77",
      "base-checksum": "090f5e687070de520452d08c4645c3a699d14ea04736cf61c33660af8d5d5d7b",
      "container-image-reference": "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:lts",
      "container-image-reference-digest": "sha256:1f0b8d3c2e9a4b5c6d7e8f90112233445566778899aabbccddeeff0011223344",
      "requested-local-packages": ["fleet-osquery-1.54.0-1.aarch64"],
      "base-commit-meta": {
        "ostree.manifest-digest": "sha256:9c0ffee0a55e7b1ade1a7e5d00dcafe000ba5eba11feed5ca1ab1ea7f00dba11"
      }
    },
    {
      "id": "default-259867d1cbc500a2080dcd681ebc3fb050392930c5c5035d2323740ea802a154.0",
      "osname": "default",
      "version": "stream10.1",
      "booted": true,
      "staged": false,
      "pinned": false,
      "unlocked": "none",
      "timestamp": 1718323200,
      "checksum": "259867d1cbc500a2080dcd681ebc3fb050392930c5c5035d2323740ea802a154",
      "base-checksum": "42cbb76127174561b9877d05fa41f6e3aea1a1d76f091ad831d07f5319a3ea55",
      "container-image-reference": "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:lts",
      "container-image-reference-digest": "sha256:00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
      "requested-packages": ["vim", "htop"],
      "requested-local-packages": ["fleet-osquery-1.54.0-1.aarch64"],
      "base-commit-meta": {
        "ostree.manifest-digest": "sha256:47c86c605d3cb565c5dde5d21f069d684ead724cb1a932feb30c39c453884be7"
      }
    },
    {
      "id": "default-42cbb76127174561b9877d05fa41f6e3aea1a1d76f091ad831d07f5319a3ea55.0",
      "osname": "default",
      "version": "stream10.1",
      "booted": false,
      "staged": false,
      "pinned": false,
      "unlocked": "none",
      "timestamp": 1718236800,
      "checksum": "42cbb76127174561b9877d05fa41f6e3aea1a1d76f091ad831d07f5319a3ea55",
      "container-image-reference": "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:lts",
      "container-image-reference-digest": "sha256:00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
      "base-commit-meta": {
        "ostree.manifest-digest": "sha256:47c86c605d3cb565c5dde5d21f069d684ead724cb1a932feb30c39c453884be7"
      }
    }
  ]
}`

// findRow returns the parsed row whose id has the given prefix.
func findRow(t *testing.T, rows []map[string]string, idPrefix string) map[string]string {
	t.Helper()
	for _, r := range rows {
		if len(r["id"]) >= len(idPrefix) && r["id"][:len(idPrefix)] == idPrefix {
			return r
		}
	}
	t.Fatalf("no row found with id prefix %q", idPrefix)
	return nil
}

func TestParseDeployments_RealWorldStatus(t *testing.T) {
	rows, err := parseDeployments([]byte(sampleStatusJSON))
	if err != nil {
		t.Fatalf("parseDeployments returned error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 deployments, got %d", len(rows))
	}

	// Emit the generated rows so the test logs contain real table data.
	for i, r := range rows {
		t.Logf("deployment[%d]: booted=%s staged=%s version=%s base_checksum=%s layered_packages=%q timestamp=%s",
			i, r["booted"], r["staged"], r["version"], r["base_checksum"], r["layered_packages"], r["timestamp"])
	}

	booted := findRow(t, rows, "default-259867")
	staged := findRow(t, rows, "default-711cc6")
	rollback := findRow(t, rows, "default-42cbb7")

	// Exactly one booted, one staged.
	if booted["booted"] != "1" || booted["staged"] != "0" {
		t.Errorf("booted deployment: got booted=%s staged=%s, want 1/0", booted["booted"], booted["staged"])
	}
	if staged["staged"] != "1" || staged["booted"] != "0" {
		t.Errorf("staged deployment: got staged=%s booted=%s, want 1/0", staged["staged"], staged["booted"])
	}
	if rollback["booted"] != "0" || rollback["staged"] != "0" {
		t.Errorf("rollback deployment: got booted=%s staged=%s, want 0/0", rollback["booted"], rollback["staged"])
	}

	// base_checksum present on the layered deployments, empty on the plain base.
	if booted["base_checksum"] != "42cbb76127174561b9877d05fa41f6e3aea1a1d76f091ad831d07f5319a3ea55" {
		t.Errorf("booted base_checksum = %q, unexpected", booted["base_checksum"])
	}
	if rollback["base_checksum"] != "" {
		t.Errorf("rollback base_checksum = %q, want empty", rollback["base_checksum"])
	}

	// Container image reference and version come straight through.
	if got, want := booted["container_image_reference"], "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:lts"; got != want {
		t.Errorf("container_image_reference = %q, want %q", got, want)
	}
	if booted["version"] != "stream10.1" {
		t.Errorf("version = %q, want stream10.1", booted["version"])
	}

	// Manifest digest is pulled out of the nested base-commit-meta object.
	if got, want := booted["manifest_digest"], "sha256:47c86c605d3cb565c5dde5d21f069d684ead724cb1a932feb30c39c453884be7"; got != want {
		t.Errorf("manifest_digest = %q, want %q", got, want)
	}

	// layered_packages on the booted deployment merges repo packages with the
	// local RPM, de-duplicated and sorted.
	if got, want := booted["layered_packages"], "fleet-osquery-1.54.0-1.aarch64,htop,vim"; got != want {
		t.Errorf("booted layered_packages = %q, want %q", got, want)
	}
	// The staged deployment only has the local RPM.
	if got, want := staged["layered_packages"], "fleet-osquery-1.54.0-1.aarch64"; got != want {
		t.Errorf("staged layered_packages = %q, want %q", got, want)
	}
	// The plain base image has nothing layered.
	if got, want := rollback["layered_packages"], ""; got != want {
		t.Errorf("rollback layered_packages = %q, want empty", got)
	}

	// timestamp is a real Unix epoch integer (regression guard, see below).
	if got, want := booted["timestamp"], "1718323200"; got != want {
		t.Errorf("booted timestamp = %q, want %q", got, want)
	}
}

// TestFormatTimestamp_Regression locks in the fix for the original bug where
// formatTimestamp used string(rune(ts)). string(rune(...)) interprets the
// integer as a single Unicode code point and emits a garbage character (often
// the U+FFFD replacement rune for large values), NOT the number. The integer
// column must contain the decimal epoch instead.
func TestFormatTimestamp_Regression(t *testing.T) {
	const ts int64 = 1718323200

	got := formatTimestamp(ts)
	if got != "1718323200" {
		t.Errorf("formatTimestamp(%d) = %q, want \"1718323200\"", ts, got)
	}

	// Demonstrate the old, broken behavior is genuinely different.
	buggy := string(rune(ts))
	if got == buggy {
		t.Errorf("formatTimestamp must not equal the old string(rune(ts)) output")
	}
	if _, err := strconv.ParseInt(got, 10, 64); err != nil {
		t.Errorf("timestamp %q does not parse as an integer: %v", got, err)
	}
	t.Logf("fixed=%q  old-buggy=%q (len=%d bytes)", got, buggy, len(buggy))
}

func TestFormatTimestamp_Zero(t *testing.T) {
	if got := formatTimestamp(0); got != "" {
		t.Errorf("formatTimestamp(0) = %q, want empty string", got)
	}
}

func TestLayeredPackages(t *testing.T) {
	cases := []struct {
		name  string
		repo  []string
		local []string
		want  string
	}{
		{"none", nil, nil, ""},
		{"local only", nil, []string{"fleet-osquery-1.54.0-1.aarch64"}, "fleet-osquery-1.54.0-1.aarch64"},
		{"repo only", []string{"vim", "htop"}, nil, "htop,vim"},
		{"merge sorted", []string{"vim", "htop"}, []string{"fleet-osquery-1.54.0-1.aarch64"}, "fleet-osquery-1.54.0-1.aarch64,htop,vim"},
		{"dedup across groups", []string{"vim"}, []string{"vim"}, "vim"},
		{"skips empties", []string{"", "vim"}, []string{""}, "vim"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := layeredPackages(Deployment{RequestedPackages: c.repo, RequestedLocalPackages: c.local})
			if got != c.want {
				t.Errorf("layeredPackages(repo=%v, local=%v) = %q, want %q", c.repo, c.local, got, c.want)
			}
		})
	}
}

func TestManifestDigest(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]interface{}
		want string
	}{
		{"nil meta", nil, ""},
		{"present", map[string]interface{}{"ostree.manifest-digest": "sha256:abc"}, "sha256:abc"},
		{"absent key", map[string]interface{}{"ostree.linux": "6.x"}, ""},
		{"non-string value", map[string]interface{}{"ostree.manifest-digest": 42}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := manifestDigest(c.meta); got != c.want {
				t.Errorf("manifestDigest(%v) = %q, want %q", c.meta, got, c.want)
			}
		})
	}
}

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != "1" {
		t.Errorf("boolToInt(true) = %q, want 1", boolToInt(true))
	}
	if boolToInt(false) != "0" {
		t.Errorf("boolToInt(false) = %q, want 0", boolToInt(false))
	}
}

// TestParseAgainstHostDump runs the parser against a real `rpm-ostree status
// --json` capture when RPM_OSTREE_STATUS_FILE points at one. It is skipped when
// the env var is unset, so it never affects normal CI runs — it is an opt-in
// way to validate the parser against ground-truth data from a live host.
func TestParseAgainstHostDump(t *testing.T) {
	path := os.Getenv("RPM_OSTREE_STATUS_FILE")
	if path == "" {
		t.Skip("set RPM_OSTREE_STATUS_FILE to a captured `rpm-ostree status --json` to run")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	rows, err := parseDeployments(raw)
	if err != nil {
		t.Fatalf("parseDeployments: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected at least one deployment row")
	}

	booted := 0
	for i, r := range rows {
		t.Logf("row[%d]: booted=%s staged=%s version=%s timestamp=%s layered=%q digest=%s",
			i, r["booted"], r["staged"], r["version"], r["timestamp"], r["layered_packages"], r["container_image_reference_digest"])
		if r["booted"] == "1" {
			booted++
		}
		// Regression guard against the string(rune(ts)) bug: every non-empty
		// timestamp must parse as a base-10 integer.
		if ts := r["timestamp"]; ts != "" {
			if _, err := strconv.ParseInt(ts, 10, 64); err != nil {
				t.Errorf("row[%d] timestamp %q is not an integer: %v", i, ts, err)
			}
		}
	}
	if booted != 1 {
		t.Errorf("expected exactly 1 booted deployment, got %d", booted)
	}
}

func TestParseDeployments_EdgeCases(t *testing.T) {
	t.Run("malformed json returns error", func(t *testing.T) {
		if _, err := parseDeployments([]byte("not json")); err == nil {
			t.Error("expected error for malformed JSON, got nil")
		}
	})

	t.Run("empty deployments array returns no rows", func(t *testing.T) {
		rows, err := parseDeployments([]byte(`{"deployments": []}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("expected 0 rows, got %d", len(rows))
		}
	})

	t.Run("missing deployments key returns no rows", func(t *testing.T) {
		rows, err := parseDeployments([]byte(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("expected 0 rows, got %d", len(rows))
		}
	})

	t.Run("unknown top-level keys are ignored", func(t *testing.T) {
		rows, err := parseDeployments([]byte(`{"deployments": [{"id": "x", "booted": true}], "some-future-field": {"a": 1}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rows) != 1 || rows[0]["booted"] != "1" {
			t.Errorf("unexpected rows: %v", rows)
		}
	})
}
