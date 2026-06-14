//go:build linux

// Package main implements an osquery extension for rpm-ostree based atomic
// hosts (Fedora Silverblue/Kinoite, Universal Blue / Bluefin, Fedora CoreOS,
// etc.). It registers one table, rpm_ostree_deployments, with one row per
// deployment as reported by `rpm-ostree status --json`.
//
// Stock osquery has no table for rpm-ostree state, so on image-mode systems
// there is no native way to see which deployment is booted, what container
// image it was sourced from, or which packages have been layered on top. This
// extension fills that gap by shelling out to rpm-ostree and parsing its JSON.
package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"

	"github.com/osquery/osquery-go"
	"github.com/osquery/osquery-go/plugin/table"
)

const tableName = "rpm_ostree_deployments"

// rpmOstreePath is the expected absolute path to the rpm-ostree binary.
// osqueryd's child environment does not always carry a useful PATH, so we
// prefer the absolute path and fall back to a PATH lookup only if it is
// missing (mirrors the ubuntu_pro extension's handling of the `pro` binary).
const rpmOstreePath = "/usr/bin/rpm-ostree"

func main() {
	// Mirror the other Linux extensions (ubuntu_pro, snap_packages): scan argv
	// for the socket flag rather than using the flag package, so osqueryd's
	// other autoload flags (--timeout, --interval, --verbose) are ignored
	// instead of aborting the process.
	var socketPath string = ":0" // default
	for i, arg := range os.Args {
		if (arg == "-socket" || arg == "--socket") && i+1 < len(os.Args) {
			socketPath = os.Args[i+1]
			break
		}
	}

	plugin := table.NewPlugin(tableName, columns(), generate)

	srv, err := osquery.NewExtensionManagerServer(tableName, socketPath)
	if err != nil {
		panic(err)
	}

	srv.RegisterPlugin(plugin)

	if err := srv.Run(); err != nil {
		panic(err)
	}
}

// rpmOstreeStatus is the subset of `rpm-ostree status --json` we consume.
// All keys are hyphenated in the JSON.
type rpmOstreeStatus struct {
	Deployments []deployment `json:"deployments"`
}

// deployment mirrors one entry of the "deployments" array.
type deployment struct {
	ID                      string `json:"id"`
	Version                 string `json:"version"`
	Checksum                string `json:"checksum"`
	Booted                  bool   `json:"booted"`
	Staged                  bool   `json:"staged"`
	ContainerImageReference string `json:"container-image-reference"`
	// Packages holds the layered packages on top of the base commit/image.
	Packages []string `json:"packages"`
}

// columns returns the schema for the rpm_ostree_deployments table.
func columns() []table.ColumnDefinition {
	return []table.ColumnDefinition{
		table.TextColumn("id"),
		table.TextColumn("version"),
		table.TextColumn("checksum"),
		table.IntegerColumn("booted"),
		table.IntegerColumn("staged"),
		table.TextColumn("container_image_reference"),
		table.TextColumn("layered_packages"),
		table.TextColumn("error"),
	}
}

// generate runs `rpm-ostree status --json` and returns one row per deployment.
// Following the ubuntu_pro convention, collection failures return a single row
// with the "error" column populated rather than a hard error, so the table is
// always queryable and the failure reason is visible in Fleet.
func generate(ctx context.Context, _ table.QueryContext) ([]map[string]string, error) {
	bin := rpmOstreePath
	if _, err := os.Stat(bin); err != nil {
		found, lookErr := exec.LookPath("rpm-ostree")
		if lookErr != nil {
			return errorRow("rpm-ostree not found - not an rpm-ostree (atomic) host"), nil
		}
		bin = found
	}

	out, err := exec.CommandContext(ctx, bin, "status", "--json").Output()
	if err != nil {
		return errorRow("failed to execute rpm-ostree status: " + err.Error()), nil
	}

	return parseStatus(out)
}

// parseStatus turns `rpm-ostree status --json` output into table rows. It is
// separated from generate so it can be unit tested against fixtures.
func parseStatus(out []byte) ([]map[string]string, error) {
	var status rpmOstreeStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return errorRow("failed to parse rpm-ostree JSON: " + err.Error()), nil
	}

	rows := make([]map[string]string, 0, len(status.Deployments))
	for _, d := range status.Deployments {
		rows = append(rows, map[string]string{
			"id":                        d.ID,
			"version":                   d.Version,
			"checksum":                  d.Checksum,
			"booted":                    boolToInt(d.Booted),
			"staged":                    boolToInt(d.Staged),
			"container_image_reference": d.ContainerImageReference,
			"layered_packages":          marshalPackages(d.Packages),
			"error":                     "",
		})
	}
	return rows, nil
}

// marshalPackages renders the layered package list as a JSON array string,
// always returning a valid array ("[]" when there are none) so consumers can
// rely on the column parsing as JSON.
func marshalPackages(pkgs []string) string {
	if len(pkgs) == 0 {
		return "[]"
	}
	b, err := json.Marshal(pkgs)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// errorRow returns a single row carrying an error message, with the remaining
// columns blank.
func errorRow(msg string) []map[string]string {
	return []map[string]string{{
		"id":                        "",
		"version":                   "",
		"checksum":                  "",
		"booted":                    "0",
		"staged":                    "0",
		"container_image_reference": "",
		"layered_packages":          "[]",
		"error":                     msg,
	}}
}

// boolToInt renders a bool as the "1"/"0" string osquery integer columns use.
func boolToInt(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
