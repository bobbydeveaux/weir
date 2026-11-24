package specproof

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// root is the repository root, from this package's directory.
const root = "../.."

var (
	requirement = regexp.MustCompile(`(?m)^### Requirement: (.+?)\s*$`)
	scenario    = regexp.MustCompile(`(?m)^#### Scenario: `)
	reference   = regexp.MustCompile("`([^`\\s:]+_test\\.go):(Test[A-Za-z0-9_]+)`")
)

// proofRows reads the table in a proof.md: requirement name to what proves it.
func proofRows(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("%s is missing: every spec needs a proof.md beside it", path)
		return nil
	}
	rows := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.SplitN(strings.Trim(strings.TrimSpace(line), "|"), "|", 2)
		if len(cells) != 2 {
			continue
		}
		name, proof := strings.TrimSpace(cells[0]), strings.TrimSpace(cells[1])
		if name == "Requirement" || strings.HasPrefix(name, "---") {
			continue
		}
		if _, twice := rows[name]; twice {
			t.Errorf("%s lists %q twice", path, name)
		}
		rows[name] = proof
	}
	return rows
}

// testsIn returns the names of the tests a file declares, read once.
var declared = map[string]map[string]bool{}

func testsIn(file string) (map[string]bool, error) {
	if got, ok := declared[file]; ok {
		return got, nil
	}
	raw, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`).FindAllStringSubmatch(string(raw), -1) {
		names[m[1]] = true
	}
	declared[file] = names
	return names, nil
}

// TestEveryRequirementSaysWhatProvesIt reads every spec and its proof. It
// fails when a requirement has no row, a row has no requirement, a row names
// neither a test nor a reason, or a test that is named does not exist.
func TestEveryRequirementSaysWhatProvesIt(t *testing.T) {
	specs, err := filepath.Glob(filepath.Join(root, "openspec", "specs", "*", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) == 0 {
		t.Fatal("there are no specs under openspec/specs")
	}
	sort.Strings(specs)
	total, proven, live := 0, 0, 0
	var unproven []string
	for _, path := range specs {
		capability := filepath.Base(filepath.Dir(path))
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// A requirement with no scenario says nothing that can be checked.
		blocks := requirement.Split(string(raw), -1)[1:]
		names := requirement.FindAllStringSubmatch(string(raw), -1)
		rows := proofRows(t, filepath.Join(filepath.Dir(path), "proof.md"))
		if rows == nil {
			continue
		}
		seen := map[string]bool{}
		for i, m := range names {
			name := m[1]
			total++
			if seen[name] {
				t.Errorf("%s: the requirement %q is written twice", capability, name)
			}
			seen[name] = true
			if !scenario.MatchString(blocks[i]) {
				t.Errorf("%s: the requirement %q has no scenario", capability, name)
			}
			proof, ok := rows[name]
			if !ok {
				t.Errorf("%s: proof.md has no row for the requirement %q", capability, name)
				continue
			}
			refs := reference.FindAllStringSubmatch(proof, -1)
			switch {
			case strings.HasPrefix(proof, "Not tested:"):
				if len(strings.TrimSpace(strings.TrimPrefix(proof, "Not tested:"))) < 20 {
					t.Errorf("%s: %q is marked not tested without saying why", capability, name)
				}
				unproven = append(unproven, capability+": "+name)
			case len(refs) == 0:
				t.Errorf("%s: the row for %q names no test as `file_test.go:TestName`, and does not start \"Not tested:\"", capability, name)
			default:
				onlyLive := true
				for _, ref := range refs {
					tests, err := testsIn(ref[1])
					if err != nil {
						t.Errorf("%s: %q names %s, which cannot be read", capability, name, ref[1])
						continue
					}
					if !tests[ref[2]] {
						t.Errorf("%s: %q names %s in %s, and there is no such test", capability, name, ref[2], ref[1])
					}
					if !strings.HasSuffix(ref[2], "Live") {
						onlyLive = false
					}
				}
				proven++
				if onlyLive {
					live++
				}
			}
		}
		for name := range rows {
			if !seen[name] {
				t.Errorf("%s: proof.md has a row for %q, which is not a requirement in spec.md", capability, name)
			}
		}
	}
	t.Logf("%d capabilities, %d requirements: %d name a test (%d of those only a live test, which CI skips), %d are not tested",
		len(specs), total, proven, live, len(unproven))
	for _, u := range unproven {
		t.Logf("not tested: %s", u)
	}
}
