package spec

import (
	"strconv"
	"strings"
)

// Literal reads an expression that is nothing but a quoted string, such as
// '"squash"', and returns the string. A handler uses it to check a fixed
// input when a definition is published; an input worked out from the run is
// checked when the step runs.
func Literal(expr any) (string, bool) {
	text, ok := expr.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if len(text) >= 2 && text[0] == '\'' && text[len(text)-1] == '\'' && !strings.ContainsAny(text[1:len(text)-1], `'"\`) {
		return text[1 : len(text)-1], true
	}
	if s, err := strconv.Unquote(text); err == nil && strings.HasPrefix(text, `"`) {
		return s, true
	}
	return "", false
}

// Warnings lists what a person publishing the definition should know and
// that does not stop it being published. Today that is one thing: a loop that
// no rule limits.
//
// A next rule may send a run back to an earlier step. If no rule on the way
// round tests a step's visit number, only the run's budget stops the loop.
func (d *Definition) Warnings() []string {
	index := map[string]int{}
	for i, s := range d.Steps {
		index[s.ID] = i
	}
	// to[i] is where a run may go from step i.
	to := make([][]int, len(d.Steps))
	for i, s := range d.Steps {
		always := false
		for _, n := range s.Next {
			if j, ok := index[n.Go]; ok {
				to[i] = append(to[i], j)
			}
			always = always || n.When == ""
		}
		if !always && !s.End && i+1 < len(d.Steps) {
			to[i] = append(to[i], i+1)
		}
	}
	var warnings []string
	for u := range d.Steps {
		for _, v := range to[u] {
			if v > u {
				continue
			}
			// u goes back to v. The loop is a way from v on to u.
			path := d.path(to, v, u)
			if path == nil || d.limited(path) {
				continue
			}
			names := make([]string, 0, len(path)+1)
			for _, i := range path {
				names = append(names, d.Steps[i].ID)
			}
			names = append(names, d.Steps[v].ID)
			warnings = append(warnings, "the loop "+strings.Join(names, " → ")+
				" has no visit limit: no rule on it tests steps.<id>.visit, so only the run's budget stops it")
		}
	}
	return warnings
}

// path returns the shortest way from one step to another, both included, or
// nil when there is none.
func (d *Definition) path(to [][]int, from, target int) []int {
	before := map[int]int{from: -1}
	queue := []int{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		if at == target {
			var path []int
			for i := at; i != -1; i = before[i] {
				path = append([]int{i}, path...)
			}
			return path
		}
		for _, next := range to[at] {
			if _, seen := before[next]; !seen {
				before[next] = at
				queue = append(queue, next)
			}
		}
	}
	return nil
}

// limited reports whether a rule of any step on the path tests a visit number.
func (d *Definition) limited(path []int) bool {
	for _, i := range path {
		for _, n := range d.Steps[i].Next {
			if strings.Contains(n.When, ".visit") {
				return true
			}
		}
	}
	return false
}
