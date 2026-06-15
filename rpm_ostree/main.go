// rpm_ostree_deployments is an osquery extension that exposes rpm-ostree
// deployment status (booted, staged, and rollback deployments) as a queryable
// table. It parses the output of `rpm-ostree status --json` and is intended for
// image-based / "immutable" Linux distributions such as Fedora Silverblue,
// CoreOS, and the Universal Blue images (Bluefin, Bazzite, etc.).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/osquery/osquery-go"
	"github.com/osquery/osquery-go/plugin/table"
)

// socketTimeout is how long to wait for osquery's extension socket to appear.
// On a cold start (e.g. an orbit restart) the extension can launch before
// osqueryd has created the socket; the osquery-go default is only 1s, which
// makes the extension exit and get respawned ("respawning too quickly"). A
// longer timeout lets it connect on the first try.
const socketTimeout = 10 * time.Second

// rpmOstreeBin is the expected absolute path to the rpm-ostree binary. osqueryd
// runs extensions with a minimal environment, so we use an absolute path first
// and only fall back to PATH lookup.
const rpmOstreeBin = "/usr/bin/rpm-ostree"

func main() {
	var socketPath string = ":0" // default
	for i, arg := range os.Args {
		if (arg == "-socket" || arg == "--socket") && i+1 < len(os.Args) {
			socketPath = os.Args[i+1]
			break
		}
	}

	plugin := table.NewPlugin("rpm_ostree_deployments", RPMOstreeColumns(), RPMOstreeGenerate)

	srv, err := osquery.NewExtensionManagerServer("rpm_ostree", socketPath, osquery.ServerTimeout(socketTimeout))
	if err != nil {
		log.Fatalf("rpm_ostree: creating extension manager server (socket %s): %v", socketPath, err)
	}

	srv.RegisterPlugin(plugin)

	if err := srv.Run(); err != nil {
		log.Fatalf("rpm_ostree: extension server stopped: %v", err)
	}
}

// RPMOstreeStatus represents the top-level JSON output from
// `rpm-ostree status --json`.
type RPMOstreeStatus struct {
	Deployments []Deployment `json:"deployments"`
}

// Deployment represents a single deployment in the rpm-ostree status output.
// Only the fields surfaced by the table are modeled; unknown JSON keys are
// ignored by encoding/json.
type Deployment struct {
	ID                            string                 `json:"id"`
	Version                       string                 `json:"version"`
	Booted                        bool                   `json:"booted"`
	Staged                        bool                   `json:"staged"`
	Pinned                        bool                   `json:"pinned"`
	Checksum                      string                 `json:"checksum"`
	BaseChecksum                  string                 `json:"base-checksum"`
	ContainerImageReference       string                 `json:"container-image-reference"`
	ContainerImageReferenceDigest string                 `json:"container-image-reference-digest"`
	OSName                        string                 `json:"osname"`
	Unlocked                      string                 `json:"unlocked"`
	Timestamp                     int64                  `json:"timestamp"`
	RequestedPackages             []string               `json:"requested-packages"`       // layered from remote repos
	RequestedLocalPackages        []string               `json:"requested-local-packages"` // layered from local RPM files
	BaseCommitMeta                map[string]interface{} `json:"base-commit-meta"`
}

// RPMOstreeColumns returns the columns for the rpm_ostree_deployments table.
func RPMOstreeColumns() []table.ColumnDefinition {
	return []table.ColumnDefinition{
		table.TextColumn("id"),
		table.TextColumn("version"),
		table.IntegerColumn("booted"),
		table.IntegerColumn("staged"),
		table.TextColumn("checksum"),
		table.TextColumn("base_checksum"),
		table.TextColumn("container_image_reference"),
		table.TextColumn("container_image_reference_digest"),
		table.TextColumn("manifest_digest"),
		table.TextColumn("layered_packages"),
		table.TextColumn("osname"),
		table.IntegerColumn("pinned"),
		table.TextColumn("unlocked"),
		table.IntegerColumn("timestamp"),
	}
}

// RPMOstreeGenerate generates the data for the rpm_ostree_deployments table. It
// shells out to rpm-ostree, then delegates parsing to parseDeployments. On any
// error it logs to stderr (captured by orbit/journalctl) and returns an empty
// result set rather than an error row, because an error row would not match the
// table schema and osquery would reject it.
func RPMOstreeGenerate(ctx context.Context, queryContext table.QueryContext) ([]map[string]string, error) {
	output, err := runRPMOstreeStatus(ctx)
	if err != nil {
		log.Printf("rpm_ostree_deployments: %v", err)
		return []map[string]string{}, nil
	}

	rows, err := parseDeployments(output)
	if err != nil {
		log.Printf("rpm_ostree_deployments: failed to parse rpm-ostree status JSON: %v", err)
		return []map[string]string{}, nil
	}

	return rows, nil
}

// runRPMOstreeStatus executes `rpm-ostree status --json` and returns its raw
// output. It prefers the absolute path and falls back to a PATH lookup.
func runRPMOstreeStatus(ctx context.Context) ([]byte, error) {
	bin := rpmOstreeBin
	if _, err := os.Stat(bin); err != nil {
		found, lookErr := exec.LookPath("rpm-ostree")
		if lookErr != nil {
			return nil, fmt.Errorf("rpm-ostree binary not found (not at %s and not on PATH)", rpmOstreeBin)
		}
		bin = found
	}

	return exec.CommandContext(ctx, bin, "status", "--json").Output()
}

// parseDeployments converts the raw JSON from `rpm-ostree status --json` into
// table rows. It is kept free of any I/O so it can be unit tested directly.
func parseDeployments(output []byte) ([]map[string]string, error) {
	var status RPMOstreeStatus
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, err
	}

	rows := make([]map[string]string, 0, len(status.Deployments))
	for _, d := range status.Deployments {
		rows = append(rows, deploymentToRow(d))
	}
	return rows, nil
}

// deploymentToRow maps a single Deployment to a table row.
func deploymentToRow(d Deployment) map[string]string {
	return map[string]string{
		"id":                               d.ID,
		"version":                          d.Version,
		"booted":                           boolToInt(d.Booted),
		"staged":                           boolToInt(d.Staged),
		"checksum":                         d.Checksum,
		"base_checksum":                    d.BaseChecksum,
		"container_image_reference":        d.ContainerImageReference,
		"container_image_reference_digest": d.ContainerImageReferenceDigest,
		"manifest_digest":                  manifestDigest(d.BaseCommitMeta),
		"layered_packages":                 layeredPackages(d),
		"osname":                           d.OSName,
		"pinned":                           boolToInt(d.Pinned),
		"unlocked":                         d.Unlocked,
		"timestamp":                        formatTimestamp(d.Timestamp),
	}
}

// layeredPackages returns a sorted, de-duplicated, comma-separated list of all
// packages layered on top of the base image — both those pulled from remote
// repositories (requested-packages) and those installed from local RPM files
// (requested-local-packages, e.g. fleet-osquery).
func layeredPackages(d Deployment) string {
	seen := make(map[string]struct{})
	pkgs := make([]string, 0, len(d.RequestedPackages)+len(d.RequestedLocalPackages))

	for _, group := range [][]string{d.RequestedPackages, d.RequestedLocalPackages} {
		for _, p := range group {
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			pkgs = append(pkgs, p)
		}
	}

	sort.Strings(pkgs)
	return strings.Join(pkgs, ",")
}

// manifestDigest extracts the OCI manifest digest from the nested
// base-commit-meta object (key "ostree.manifest-digest"), returning "" if it is
// absent or not a string.
func manifestDigest(meta map[string]interface{}) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta["ostree.manifest-digest"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// boolToInt converts a bool to "0"/"1" for an osquery INTEGER column.
func boolToInt(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// formatTimestamp renders a Unix epoch timestamp as its decimal string. An
// empty string is returned for a zero timestamp so the column is NULL-ish
// rather than "0".
func formatTimestamp(ts int64) string {
	if ts == 0 {
		return ""
	}
	return strconv.FormatInt(ts, 10)
}
