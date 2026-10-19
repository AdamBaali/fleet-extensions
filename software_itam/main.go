// software_itam is an osquery extension that fills the gaps between Fleet's
// software inventory and what a ServiceNow ITAM export needs: a publisher name
// and an install date for each app or package, plus the first time this host
// was seen with it. It reads the same native sources osquery tables read
// (Info.plist, Spotlight, code signatures, the Uninstall registry keys, the RPM
// and dpkg databases) but returns one row per item with the best date already
// picked, so the exporter doesn't have to join several reports.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/osquery/osquery-go"
	"github.com/osquery/osquery-go/plugin/table"
)

// socketTimeout is how long to wait for osquery's extension socket to appear.
// On a cold start (e.g. an orbit restart) the extension can launch before
// osqueryd has created the socket; the osquery-go default is only 1s, which
// makes the extension exit and get respawned ("respawning too quickly").
const socketTimeout = 10 * time.Second

// stateFileName sits next to the binary because that directory is root-only
// on every platform the installers use, so other users can't forge first_seen.
const stateFileName = "software_itam_state.json"

// mu serializes queries. Two overlapping runs would race on the state file and
// spawn codesign twice for the same app.
var mu sync.Mutex

func main() {
	socketPath := ":0"
	for i, arg := range os.Args {
		if (arg == "-socket" || arg == "--socket") && i+1 < len(os.Args) {
			socketPath = os.Args[i+1]
			break
		}
	}

	plugin := table.NewPlugin("software_itam", Columns(), Generate)

	srv, err := osquery.NewExtensionManagerServer("software_itam", socketPath, osquery.ServerTimeout(socketTimeout))
	if err != nil {
		log.Fatalf("software_itam: creating extension manager server (socket %s): %v", socketPath, err)
	}
	srv.RegisterPlugin(plugin)
	if err := srv.Run(); err != nil {
		log.Fatalf("software_itam: extension server stopped: %v", err)
	}
}

// Generate collects items for this OS and turns them into rows. Errors are
// logged and an empty or partial result is returned, because an error from a
// table plugin makes the whole Fleet report run fail on that host.
func Generate(ctx context.Context, _ table.QueryContext) ([]map[string]string, error) {
	mu.Lock()
	defer mu.Unlock()

	now := time.Now().Unix()
	path := statePath()
	st, err := loadState(path)
	if err != nil {
		log.Printf("software_itam: state %s: %v (first_seen restarts from now)", path, err)
	}

	items := collect(ctx, st, now)
	rows := buildRows(items, st, now)

	st.Prune(now)
	if err := saveState(path, st); err != nil {
		log.Printf("software_itam: saving state %s: %v", path, err)
	}
	return rows, nil
}

func statePath() string {
	exe, err := os.Executable()
	if err != nil {
		return stateFileName
	}
	return filepath.Join(filepath.Dir(exe), stateFileName)
}
