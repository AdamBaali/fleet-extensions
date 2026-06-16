# rpm_ostree Extension: Building osquery Tables for Immutable Linux

## Talk Outline for Tuesday

### Opening: The Problem We're Solving

**Context:** rpm-ostree is an image-based package system used by Fedora Silverblue, Bluefin, and other "immutable" Linux distros. Fleet (osquery) can't see deployment status without custom extensions.

**The Gap:** 
- Fleet sees running processes, network, packages (traditional dnf/rpm)
- Fleet does NOT see: which deployment is booted, pending updates, staged deployments, rollback availability
- For compliance/audit: you need visibility into image versions and deployment changes

**Solution:** Build an osquery extension that parses `rpm-ostree status --json` and exposes deployments as queryable table.

---

### Section 1: What We Built

#### The Table: `rpm_ostree_deployments`

**Columns:**
```
id                          text      Deployment ID (includes checksum)
version                     text      Version string (e.g., "stream10.1")
booted                      integer   1 if currently running, 0 otherwise
staged                      integer   1 if pending next boot, 0 otherwise
checksum                    text      Commit checksum
base_checksum               text      Base image checksum
container_image_reference   text      Container image URL (ghcr.io/ublue-os/bluefin:lts)
container_image_reference_digest text  SHA256 digest of image
manifest_digest             text      OSTree manifest digest
layered_packages            text      Comma-separated requested packages
osname                      text      OS name ("default" typically)
pinned                      integer   1 if pinned, 0 otherwise
unlocked                    text      Unlocked state
timestamp                   integer   Deployment timestamp
```

**Example Query:**
```sql
SELECT id, version, booted, staged, container_image_reference 
FROM rpm_ostree_deployments 
WHERE booted = 1;
```

**Expected Result (3 rows per host):**
1. Currently booted deployment (booted=1, staged=0)
2. Staged for next boot (booted=0, staged=1)
3. Rollback/previous (booted=0, staged=0)

---

### Section 2: How We Built It

#### Phase 0: Baseline & Discovery

**Method:** SSH to host, ran actual commands to understand the real data structure.

```bash
# Discover the data source
ssh bluefin 'rpm-ostree status --json | jq'

# Check what fields actually exist
ssh bluefin 'rpm-ostree status --json | jq '.deployments[0] | keys'
```

**Key Learning:** Field names use HYPHENS in JSON (`base-checksum`, `container-image-reference`), not underscores. This matters for struct tags.

#### Phase 1: Data Validation

**Findings:**
- Three deployments present (booted, staged, rollback)
- Booleans are JSON `true`/`false`, not integers
- Some fields are nested objects (e.g., `base-commit-meta["ostree.manifest-digest"]`)
- No external API—parse JSON directly from subprocess output

#### Phase 2: Implementation

**Tech Stack:**
- Language: Go 1.26
- SDK: github.com/osquery/osquery-go
- Target: Linux arm64 (Bluefin is Apple Silicon-based VM)

**Key Code Patterns:**

1. **Struct for JSON parsing** (with hyphen tags):
```go
type Deployment struct {
    ID                        string   `json:"id"`
    Version                   string   `json:"version"`
    Booted                    bool     `json:"booted"`
    BaseChecksum              string   `json:"base-checksum"`
    ContainerImageReference   string   `json:"container-image-reference"`
    BaseCommitMeta            map[string]interface{} `json:"base-commit-meta"`
}
```

2. **Table schema definition** (convert hyphens to underscores):
```go
func RPMOstreeColumns() []table.ColumnDefinition {
    return []table.ColumnDefinition{
        table.TextColumn("id"),
        table.IntegerColumn("booted"),
        table.TextColumn("base_checksum"),
        // ...
    }
}
```

3. **Generate function** (execute, parse, return rows):
```go
func RPMOstreeGenerate(ctx context.Context, queryContext table.QueryContext) ([]map[string]string, error) {
    // Use full path: /usr/bin/rpm-ostree
    cmd := exec.CommandContext(ctx, "/usr/bin/rpm-ostree", "status", "--json")
    output, err := cmd.Output()
    
    var status RPMOstreeStatus
    json.Unmarshal(output, &status)
    
    for _, deployment := range status.Deployments {
        row := map[string]string{
            "booted": boolToInt(deployment.Booted),
            // Extract nested fields, join arrays, etc.
        }
        results = append(results, row)
    }
    return results, nil
}
```

#### Phase 3: Integration with Fleet/Orbit

**How it works:**
1. Compile for target architecture: `GOARCH=arm64 GOOS=linux CGO_ENABLED=0 go build -o rpm_ostree-arm64.ext`
2. Install to `/var/lib/fleetd/extensions/rpm_ostree.ext` (root:root, 0700 perms)
3. Add to osqueryd flags: `--extension=/var/lib/fleetd/extensions/rpm_ostree.ext`
4. Restart orbit.service
5. Extension registers with osqueryd via named socket
6. Fleet's distributed queries can now SELECT from `rpm_ostree_deployments`

---

### Section 3: Challenges & Solutions

#### Challenge 1: Field Name Mapping
**Problem:** JSON has hyphens, osquery tables use underscores
**Solution:** Struct tags specify JSON field names; Go code uses underscores
```go
// JSON: "base-checksum" → Go struct field: BaseChecksum
BaseChecksum string `json:"base-checksum"`
// Column definition: "base_checksum" (underscore in table)
```

#### Challenge 2: Boolean Handling
**Problem:** JSON booleans, but osquery INTEGER columns want "0"/"1" strings
**Solution:** Helper function to convert
```go
func boolToInt(b bool) string {
    if b { return "1" }
    return "0"
}
```

#### Challenge 3: Nested Object Extraction
**Problem:** `manifest-digest` is inside `base-commit-meta` object
**Solution:** Type assertion in Generate function
```go
if md, ok := deployment.BaseCommitMeta["ostree.manifest-digest"]; ok {
    if mdStr, ok := md.(string); ok {
        manifestDigest = mdStr
    }
}
```

#### Challenge 4: PATH Issue in osqueryd Context
**Problem:** Extension called `rpm-ostree` without full path; failed in osqueryd context
**Solution:** Use full path `/usr/bin/rpm-ostree`
```go
// Before (fails): exec.CommandContext(ctx, "rpm-ostree", ...)
// After (works): exec.CommandContext(ctx, "/usr/bin/rpm-ostree", ...)
```

#### Challenge 5: Schema Mismatch
**Problem:** Error handling returned `{"error": "..."}` rows, but "error" column didn't exist in table schema
**Solution:** Return empty results on error instead of error rows
```go
// Return empty results, not error column
return []map[string]string{}, nil
```

---

### Section 4: Deployment & Testing

#### Deployment Script
Provided as `deploy.sh` — automates:
1. Cross-compile for target arch
2. Copy binary to host via scp
3. Install with proper permissions
4. Create extensions.load
5. Update osquery.flags
6. Restart orbit service
7. Verify registration

#### Testing
1. **Standalone:** ephemeral osqueryd socket (discovered osqueryi not available)
2. **Integrated:** Through orbit with real Fleet instance
3. **End-to-end:** Fleet live query returned real deployment data

**Validation:**
- Extension loads: Check `journalctl` for "Registering extension (rpm_ostree...)"
- Extension works: Run query, get 3 rows with real deployment info
- No errors: EROFS (read-only) and permission errors in logs

---

### Section 5: Key Takeaways

1. **Extensions bridge the gap** between osquery's built-in tables and system-specific state
2. **Data discovery first** — understand the actual JSON/command output before writing code
3. **Type safety matters** — struct tags map JSON fields precisely; no guessing
4. **Full paths** — osqueryd runs in different context; use absolute paths for external commands
5. **Schema consistency** — table columns must exist; error rows without columns get rejected
6. **Testing strategy** — verify each layer (command output, JSON parse, table rows, Fleet query)

---

### Section 6: Use Cases

**Who benefits:**
- **Compliance:** Track image versions and deployments across fleet
- **Incident response:** Query "which hosts have pending updates?" across 1000+ machines
- **Audit:** Report deployment history, staged changes, rollback availability
- **Automation:** Trigger workflows based on deployment state (e.g., "alert if 100+ hosts staged for update")

**Example queries:**
```sql
-- Find all hosts with staged updates
SELECT hostname, version FROM rpm_ostree_deployments WHERE staged = 1;

-- Count deployments by version
SELECT version, COUNT(*) FROM rpm_ostree_deployments GROUP BY version;

-- Find rollback candidates
SELECT hostname, version FROM rpm_ostree_deployments WHERE booted = 0 AND staged = 0;
```

---

### Section 7: Code & Resources

**Repository:** `/Users/adam/Documents/GitHub/fleet-extensions/rpm_ostree/`

**Files:**
- `main.go` — Extension implementation (150 lines)
- `Makefile` — Build for amd64 & arm64
- `go.mod` / `go.sum` — Dependencies (just osquery-go SDK)
- `README.md` — Usage & schema
- `deploy.sh` — Deployment automation
- `TESTING.md` — Test scenarios

**Git commits:**
- `0b7713c` — Fix: PATH and schema bugs
- `2075eb2` — Initial: rpm_ostree extension

---

### Section 8: Timeline & Stats

**Development:**
- Evening session (June 15, 2026)
- 3 phases: baseline → learning → implementation
- 2 bug fixes (PATH, schema mismatch)
- 1 successful end-to-end validation

**Stats:**
- 6.9 MB binary (Go + osquery SDK)
- ~150 lines of code
- 13 columns in table
- 3 deployments per host (booted, staged, rollback)

---

### Closing: Why This Matters

**The journey from "I don't see this data" to "I can query it"** happens in hours, not months, with osquery extensions. The learning curve is real (field name mapping, type conversions, context issues), but each solution is reusable for your next system-specific table.

For rpm-ostree systems, this extension is a foundation for visibility into:
- Deployment status (live vs. pending)
- Container image lineage
- Update rollback availability
- System immutability and integrity

**Next:** Build similar extensions for your other systems, compose multi-table queries, embed in automated workflows.

---

## Talk Slide Outline

**Slide 1:** Title + Problem
- "Making rpm-ostree visible to Fleet osquery"
- Gap: Fleet can't see image deployments

**Slide 2:** The Table
- Schema (13 columns)
- 3 rows per host (booted, staged, rollback)

**Slide 3:** Data Discovery
- Real command: `rpm-ostree status --json`
- Real output: 3 deployments, nested objects

**Slide 4:** Implementation Path
- Struct → JSON tags
- Table → Column definitions  
- Generate → Execute + Parse + Return

**Slide 5:** Integration with Fleet
- Extension binary + flags + restart
- osqueryd socket communication
- Fleet's distributed queries

**Slide 6:** Challenges & Solutions
- 5 specific problems with fixes
- (field names, booleans, nesting, PATH, schema)

**Slide 7:** Deployment & Results
- deploy.sh automation
- Fleet query output: 3 real rows
- No errors in logs

**Slide 8:** Use Cases
- 4 real-world queries
- Compliance, incident response, audit, automation

**Slide 9:** Takeaways + Resources
- Code, git commits, TESTING.md
- Timeline & stats

**Slide 10:** Q&A
- Extension as template for other systems

---

## Appendix: Command Reference

```bash
# Cross-compile
GOARCH=arm64 GOOS=linux CGO_ENABLED=0 go build -o rpm_ostree-arm64.ext .

# Deploy
./deploy.sh bluefin adam <password>

# Verify on host
ssh bluefin 'journalctl -b -u orbit.service | grep rpm_ostree'
ssh bluefin 'ps aux | grep rpm_ostree.ext'

# Query in Fleet
SELECT * FROM rpm_ostree_deployments LIMIT 3;
```

---

**Total talk duration:** 15-20 minutes + 5-10 min Q&A
**Audience:** Platform/SRE teams using osquery, rpm-ostree, Fleet

---

## ADDENDUM — Session 2: Testing & Hardening (slide-ready)

> Narrative for the talk: the extension was already registering with osquery and
> returning real rows in Fleet — but writing actual tests surfaced a latent bug
> that would have bitten the moment anyone queried the `timestamp` column. Good
> story arc: "it worked in the demo, then the tests found the bug."

### New slide: "Then we wrote tests"

- **Made it testable.** Split the table into two pieces: `RPMOstreeGenerate` (shells
  out to `rpm-ostree`) and `parseDeployments([]byte)` (pure — JSON in, rows out).
  The parser has zero I/O, so it runs on a laptop with no rpm-ostree host.
- **Fixture from real data.** The test input is a reconstructed `rpm-ostree status
  --json` using the actual deployment IDs, checksums, version, and image pulled from
  the Bluefin host's live query — so the tests assert against ground truth.
- **Result:** 8 tests / 16 subtests, all green. `go vet` + `gofmt` clean. Builds for
  both amd64 and arm64 (static ELF).

### New slide: "The bug the tests caught" (the money slide)

```go
// Before — compiles fine, silently wrong:
return string(rune(ts))        // 1718323200  ->  "�"  (U+FFFD, 3 bytes)

// After:
return strconv.FormatInt(ts, 10)   // 1718323200  ->  "1718323200"
```

- `string(rune(int))` is legal Go — it reads the integer as a *Unicode code point*,
  not a number. A real epoch becomes a single replacement character.
- It hid because the production query never selected `timestamp`. The test that
  prints `fixed="1718323200"  old-buggy="�"` is a great live demo.

### New slide: "Hardening for the PR"

- `layered_packages` now merges **repo-layered** (`requested-packages`) **and local
  RPM** (`requested-local-packages`) packages, de-duplicated and sorted — before it
  only showed local RPMs, so a `rpm-ostree install vim` was invisible.
- Errors (missing binary, bad JSON) now **log to stderr** (visible in `journalctl`)
  and return zero rows instead of a schema-breaking error row.
- Absolute path to `rpm-ostree` with a PATH fallback.
- **Cold-start race fixed.** On every orbit restart the extension launched before
  osqueryd's socket existed; the SDK's default 1-second wait expired and it
  `panic`ed — `Extension respawning too quickly` in the logs. Fix: one option,
  `osquery.ServerTimeout(10*time.Second)`, so it waits for the socket and connects
  first try. Good "read the SDK source" moment for the talk.

### New slide: "Deployed it and watched it work" (the proof)

- Cross-compiled, `scp`'d to the Bluefin host, checksum-verified, installed
  root:root 0700, restarted orbit — then ran the query **live through the Fleet API**
  (the campaign endpoint + the live-query results **websocket**, same path the UI uses).
- 3 rows came back, `status: finished`, and every field was **diffed against the
  host's own `rpm-ostree status --json`** — exact match. The `timestamp` column now
  shows real epochs (`1778591568`) where it used to be `�`.
- Closing line: *"the unit test caught the bug; the live deploy proved the fix."*

### Takeaways to add to the closing slide

1. **Compiling ≠ correct.** `string(rune(int))` is a silent, legal footgun in Go.
2. **Separate the subprocess from the parser** — then you can unit-test the parser
   against captured real output, on any OS.
3. **Build the fixture from real `--json`**, not from what you *think* the schema is.
4. **Make the table fail loud in the logs, quiet in the schema** — log the error,
   return no rows.
