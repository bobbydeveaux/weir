package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/spec"
)

// Reading a pull request's checks.
//
// Which checks decide is a choice the step makes. By default they are the
// checks GitHub says are required for that pull request, so that an optional
// check cannot hold a merge up. A repository with no rules requires nothing,
// and there the step says "count: all": every check on the head commit
// decides. A step may also name checks, which must then really have run.
//
// Whether a check is required is GitHub's own answer, never worked out here
// from the repository's settings.

const (
	countRequired = "required"
	countAll      = "all"

	// maxCheckLog is how much of the end of a failed job's log is kept.
	maxCheckLog = 16 << 10
	// maxChecksOutput is the most a step's output may hold.
	maxChecksOutput = 256 << 10
	// lowRateLimit is how few calls may be left before a wait is stretched
	// to the time the limit resets.
	lowRateLimit = 50
)

// pullChecksQuery asks for a pull request, the checks on its head commit and
// whether GitHub requires each of them.
const pullChecksQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){
id state mergeStateStatus baseRefName headRefOid mergeCommit{oid}
baseRef{refUpdateRule{requiredStatusCheckContexts}}
commits(last:1){nodes{commit{
checkSuites(first:50){nodes{status workflowRun{databaseId} checkRuns{totalCount}}}
statusCheckRollup{contexts(first:100){nodes{__typename
... on CheckRun{name status conclusion detailsUrl databaseId summary isRequired(pullRequestNumber:$number) checkSuite{app{slug}}}
... on StatusContext{context state targetUrl description isRequired(pullRequestNumber:$number)}
}}}}}}}}}`

// check is one check run or commit status on a head commit.
type check struct {
	Name       string
	URL        string
	Conclusion string // lower case; empty while the check is unfinished
	Summary    string
	Required   bool
	// job is the GitHub Actions job behind a check run, whose log can be read.
	job int64
}

// pull is a pull request as the checks and merge handlers see it.
type pull struct {
	ID          string
	State       string // OPEN, CLOSED or MERGED
	Base, Head  string
	MergeState  string
	MergeCommit string
	Checks      []check
	// Expected are the checks the base branch requires, whether or not one
	// has been reported yet.
	Expected []string
	// OpenSuites is how many sets of checks have started and not finished.
	OpenSuites int
}

// verdict is what the checks on a pull request add up to.
type verdict struct {
	Counted    []check
	Failed     []check
	Other      []check
	Unfinished []string
}

// passes reports whether a finished check counts as passed. GitHub accepts a
// check that was skipped or is neutral; a check the step named must really
// have succeeded.
func passes(conclusion string, named bool) bool {
	if named {
		return conclusion == "success"
	}
	return conclusion == "success" || conclusion == "skipped" || conclusion == "neutral"
}

// judge sorts a pull request's checks into those that decide and those that
// do not, and says which failed and which are still to finish.
func judge(p *pull, count string, named []string) verdict {
	var v verdict
	isNamed := map[string]bool{}
	for _, n := range named {
		isNamed[n] = true
	}
	seen := map[string]bool{}
	for _, c := range p.Checks {
		seen[c.Name] = true
		if !(count == countAll || c.Required || isNamed[c.Name]) {
			v.Other = append(v.Other, c)
			continue
		}
		v.Counted = append(v.Counted, c)
		switch {
		case c.Conclusion == "":
			v.Unfinished = append(v.Unfinished, c.Name)
		case !passes(c.Conclusion, isNamed[c.Name]):
			v.Failed = append(v.Failed, c)
		}
	}
	// A check that must run and has not been reported yet is still to come.
	for _, name := range append(append([]string{}, p.Expected...), named...) {
		if !seen[name] {
			seen[name] = true
			v.Unfinished = append(v.Unfinished, name)
		}
	}
	if count == countAll && p.OpenSuites > 0 && len(v.Unfinished) == 0 {
		v.Unfinished = append(v.Unfinished, "a set of checks that has started")
	}
	return v
}

// request makes one call as the connection and returns the reply whole. It
// follows no redirect, so a credential never goes to another host.
func (g *github) request(ctx context.Context, method, address string, body any) (int, http.Header, []byte, error) {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, nil, err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, payload)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "weir")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.to.Auth.Kind == secrets.GitHubApp {
		token, err := g.reg.GitHubToken(ctx, g.client, g.to, g.repo, g.needs)
		if err != nil {
			return 0, nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	} else if err := g.reg.sign(req, g.to); err != nil {
		return 0, nil, nil, err
	}
	client := *g.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("GitHub could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, resp.Header, raw, nil
}

// graphql asks GitHub's GraphQL API. It returns the data, and the messages
// of any errors GitHub gave in place of an answer.
func (g *github) graphql(ctx context.Context, query string, vars map[string]any, out any) (http.Header, []string, error) {
	address := g.api + "/graphql"
	if strings.HasSuffix(g.api, "/v3") { // GitHub Enterprise Server
		address = strings.TrimSuffix(g.api, "/v3") + "/graphql"
	}
	status, header, raw, err := g.request(ctx, http.MethodPost, address, map[string]any{"query": query, "variables": vars})
	if err != nil {
		return nil, nil, err
	}
	var reply struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &reply)
	if status >= 300 {
		if reply.Message == "" {
			reply.Message = http.StatusText(status)
		}
		return header, nil, fmt.Errorf("GitHub said %d: %s", status, reply.Message)
	}
	var problems []string
	for _, e := range reply.Errors {
		problems = append(problems, e.Message)
	}
	if len(problems) == 0 && out != nil {
		if err := json.Unmarshal(reply.Data, out); err != nil {
			return header, nil, fmt.Errorf("GitHub's reply was not understood: %w", err)
		}
	}
	return header, problems, nil
}

// pullChecks reads a pull request and the checks on its head commit.
func (g *github) pullChecks(ctx context.Context, number int) (*pull, http.Header, error) {
	owner, name, _ := strings.Cut(g.repo, "/")
	type node struct {
		Type        string `json:"__typename"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		Conclusion  string `json:"conclusion"`
		DetailsURL  string `json:"detailsUrl"`
		DatabaseID  int64  `json:"databaseId"`
		Summary     string `json:"summary"`
		IsRequired  bool   `json:"isRequired"`
		Context     string `json:"context"`
		State       string `json:"state"`
		TargetURL   string `json:"targetUrl"`
		Description string `json:"description"`
		CheckSuite  struct {
			App struct {
				Slug string `json:"slug"`
			} `json:"app"`
		} `json:"checkSuite"`
	}
	var data struct {
		Repository struct {
			PullRequest *struct {
				ID               string `json:"id"`
				State            string `json:"state"`
				MergeStateStatus string `json:"mergeStateStatus"`
				BaseRefName      string `json:"baseRefName"`
				HeadRefOid       string `json:"headRefOid"`
				MergeCommit      *struct {
					Oid string `json:"oid"`
				} `json:"mergeCommit"`
				BaseRef *struct {
					RefUpdateRule *struct {
						Contexts []string `json:"requiredStatusCheckContexts"`
					} `json:"refUpdateRule"`
				} `json:"baseRef"`
				Commits struct {
					Nodes []struct {
						Commit struct {
							CheckSuites struct {
								Nodes []struct {
									Status      string `json:"status"`
									WorkflowRun *struct {
										DatabaseID int64 `json:"databaseId"`
									} `json:"workflowRun"`
									CheckRuns struct {
										TotalCount int `json:"totalCount"`
									} `json:"checkRuns"`
								} `json:"nodes"`
							} `json:"checkSuites"`
							Rollup *struct {
								Contexts struct {
									Nodes []node `json:"nodes"`
								} `json:"contexts"`
							} `json:"statusCheckRollup"`
						} `json:"commit"`
					} `json:"nodes"`
				} `json:"commits"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	header, problems, err := g.graphql(ctx, pullChecksQuery, map[string]any{"owner": owner, "name": name, "number": number}, &data)
	if err != nil {
		return nil, header, err
	}
	if len(problems) > 0 {
		return nil, header, Permanent(fmt.Errorf("GitHub would not show pull request %d of %s: %s", number, g.repo, strings.Join(problems, "; ")))
	}
	pr := data.Repository.PullRequest
	if pr == nil {
		return nil, header, Permanent(fmt.Errorf("%s has no pull request %d", g.repo, number))
	}
	p := &pull{ID: pr.ID, State: pr.State, Base: pr.BaseRefName, Head: pr.HeadRefOid, MergeState: strings.ToLower(pr.MergeStateStatus)}
	if p.MergeState == "" {
		p.MergeState = "unknown"
	}
	if pr.MergeCommit != nil {
		p.MergeCommit = pr.MergeCommit.Oid
	}
	if pr.BaseRef != nil && pr.BaseRef.RefUpdateRule != nil {
		p.Expected = pr.BaseRef.RefUpdateRule.Contexts
	}
	p.Expected = append(p.Expected, g.rulesetChecks(ctx, p.Base)...)
	if len(pr.Commits.Nodes) == 0 {
		return p, header, nil
	}
	commit := pr.Commits.Nodes[0].Commit
	for _, s := range commit.CheckSuites.Nodes {
		// A set of checks that an installed App never fills stays queued for
		// ever, and is not waited for.
		if s.Status != "COMPLETED" && (s.WorkflowRun != nil || s.CheckRuns.TotalCount > 0) {
			p.OpenSuites++
		}
	}
	if commit.Rollup == nil {
		return p, header, nil
	}
	for _, n := range commit.Rollup.Contexts.Nodes {
		c := check{Name: n.Name, URL: n.DetailsURL, Summary: n.Summary, Required: n.IsRequired}
		if n.Type == "StatusContext" {
			c.Name, c.URL, c.Summary = n.Context, n.TargetURL, n.Description
			if n.State != "PENDING" && n.State != "EXPECTED" {
				c.Conclusion = strings.ToLower(n.State)
			}
		} else if n.Status == "COMPLETED" {
			c.Conclusion = strings.ToLower(n.Conclusion)
			if n.CheckSuite.App.Slug == "github-actions" {
				c.job = n.DatabaseID
			}
		}
		p.Checks = append(p.Checks, c)
	}
	return p, header, nil
}

// rulesetChecks returns the checks a branch's rulesets require. A reply that
// cannot be had is no rules: GitHub still marks each check as required or
// not, and this only adds the ones that have not been reported yet.
func (g *github) rulesetChecks(ctx context.Context, branch string) []string {
	var rules []struct {
		Type       string `json:"type"`
		Parameters struct {
			Checks []struct {
				Context string `json:"context"`
			} `json:"required_status_checks"`
		} `json:"parameters"`
	}
	if _, err := g.do(ctx, http.MethodGet, "/rules/branches/"+branch, nil, &rules); err != nil {
		return nil
	}
	var names []string
	for _, r := range rules {
		if r.Type != "required_status_checks" {
			continue
		}
		for _, c := range r.Parameters.Checks {
			names = append(names, c.Context)
		}
	}
	return names
}

// jobLog returns the end of a GitHub Actions job's log, or nothing when it
// cannot be read. GitHub answers with the address of the log, which is on
// another host and is fetched without the credential.
func (g *github) jobLog(ctx context.Context, job int64) string {
	status, header, raw, err := g.request(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/actions/jobs/%d/logs", g.api, g.repo, job), nil)
	if err != nil {
		return ""
	}
	if status >= 300 && status < 400 && header.Get("Location") != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, header.Get("Location"), nil)
		if err != nil {
			return ""
		}
		plain := &http.Client{Timeout: g.client.Timeout, Transport: g.client.Transport, CheckRedirect: secrets.SameHost}
		resp, err := plain.Do(req)
		if err != nil {
			return ""
		}
		defer resp.Body.Close()
		raw, _ = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		status = resp.StatusCode
	}
	if status != http.StatusOK {
		return ""
	}
	if len(raw) > maxCheckLog {
		raw = raw[len(raw)-maxCheckLog:]
	}
	return strings.ToValidUTF8(string(raw), "")
}

// onBase says how the newest finished check of each name stands on a branch:
// true when it passed.
func (g *github) onBase(ctx context.Context, branch string) map[string]bool {
	stands := map[string]bool{}
	var runs struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	if _, err := g.do(ctx, http.MethodGet, "/commits/"+branch+"/check-runs?filter=latest&per_page=100", nil, &runs); err == nil {
		for _, r := range runs.CheckRuns {
			if r.Status == "completed" {
				stands[r.Name] = passes(r.Conclusion, false)
			}
		}
	}
	var combined struct {
		Statuses []struct {
			Context string `json:"context"`
			State   string `json:"state"`
		} `json:"statuses"`
	}
	if _, err := g.do(ctx, http.MethodGet, "/commits/"+branch+"/status", nil, &combined); err == nil {
		for _, s := range combined.Statuses {
			if s.State != "pending" {
				stands[s.Context] = s.State == "success"
			}
		}
	}
	return stands
}

// checksInput is what "github.checks" and the merge handlers read about
// which checks decide.
type checksInput struct {
	Connection  string   `json:"connection"`
	Repo        string   `json:"repo"`
	PullRequest int      `json:"pull_request"`
	Count       string   `json:"count"`
	Checks      []string `json:"checks"`
	// Every is how long to wait before looking again, and Settle is how long
	// a commit with no checks is given for one to appear.
	Every  string `json:"every"`
	Settle string `json:"settle"`
}

func (in *checksInput) check() error {
	if in.Count == "" {
		in.Count = countRequired
	}
	switch {
	case in.PullRequest <= 0:
		return errors.New(`say which pull request, as "pull_request", its number`)
	case in.Count != countRequired && in.Count != countAll:
		return fmt.Errorf("count %q is not one of required, all", in.Count)
	}
	return nil
}

// duration reads a step's own duration, or gives the default.
func duration(text string, otherwise time.Duration) (time.Duration, error) {
	if text == "" {
		return otherwise, nil
	}
	return spec.ParseDuration(text)
}

// nextLook says how long to wait before reading the checks again. Each wait
// is longer than the last, up to ten times the first, and a token that is
// nearly out of calls waits for its limit to reset.
func nextLook(every time.Duration, tries int, header http.Header, now time.Time) time.Duration {
	wait := every
	for i := 0; i < tries && wait < 10*every; i++ {
		wait = wait * 3 / 2
	}
	if wait > 10*every {
		wait = 10 * every
	}
	left, err := strconv.Atoi(header.Get("X-RateLimit-Remaining"))
	reset, err2 := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64)
	if err == nil && err2 == nil && left <= lowRateLimit {
		if until := time.Unix(reset, 0).Sub(now); until > wait {
			wait = until
		}
	}
	return wait
}

func checkNames(checks []check) []string {
	names := make([]string, 0, len(checks))
	for _, c := range checks {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return names
}

func brief(checks []check) []map[string]any {
	out := make([]map[string]any, 0, len(checks))
	for _, c := range checks {
		conclusion := c.Conclusion
		if conclusion == "" {
			conclusion = "unfinished"
		}
		out = append(out, map[string]any{"name": c.Name, "conclusion": conclusion, "required": c.Required})
	}
	return out
}

// checkRules is what publishing asks of a step that names "github.checks".
func checkRules(s *spec.Step) []string {
	var problems []string
	if s.Timeout == "" {
		problems = append(problems, "a step that waits on checks must set a timeout")
	}
	return append(problems, countRule(s)...)
}

func countRule(s *spec.Step) []string {
	if count, ok := spec.Literal(s.Input["count"]); ok && count != countRequired && count != countAll {
		return []string{fmt.Sprintf("count %q is not one of required, all", count)}
	}
	return nil
}

// RegisterGitHubChecks adds "github.checks", a deterministic handler that
// waits for the checks on a pull request and says whether they passed.
//
// While a check that decides is unfinished it asks to be run again, so the
// step's timeout is the limit on the wait. It can write nothing.
func RegisterGitHubChecks(reg *Registry, eng *engine.Engine, client *http.Client) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: secrets.SameHost}
	}
	reg.RegisterRules("github.checks", checkRules)
	reg.Register("github.checks", func(ctx context.Context, call Call) (json.RawMessage, error) {
		var in checksInput
		if err := json.Unmarshal(call.Input, &in); err != nil {
			return nil, Permanent(fmt.Errorf("the step's input could not be read: %w", err))
		}
		if err := in.check(); err != nil {
			return nil, Permanent(err)
		}
		every, err := duration(in.Every, 30*time.Second)
		if err != nil {
			return nil, Permanent(fmt.Errorf("every: %w", err))
		}
		settle, err := duration(in.Settle, time.Minute)
		if err != nil {
			return nil, Permanent(fmt.Errorf("settle: %w", err))
		}
		g, err := githubFor(ctx, reg, eng, client, call.RunID, in.Connection, in.Repo)
		if err != nil {
			return nil, err
		}
		g.needs = GitHubChecks
		p, header, err := g.pullChecks(ctx, in.PullRequest)
		if err != nil {
			return nil, err
		}
		v := judge(p, in.Count, in.Checks)
		// Straight after a push no check exists yet. With every check
		// counting, that is not yet "no checks".
		early := in.Count == countAll && len(v.Counted) == 0 && call.Waited < settle
		if p.State == "OPEN" && (len(v.Unfinished) > 0 || early) {
			return nil, NotYet(nextLook(every, call.Tries, header, time.Now()))
		}
		return json.Marshal(g.report(ctx, p, v))
	})
}

// report is the output of "github.checks" once every check that decides has
// finished.
func (g *github) report(ctx context.Context, p *pull, v verdict) map[string]any {
	state := "passed"
	switch {
	case len(v.Failed) > 0:
		state = "failed"
	case len(v.Counted) == 0:
		state = "none"
	}
	out := map[string]any{"state": state, "head_sha": p.Head, "base": p.Base, "mergeable_state": p.MergeState,
		"pull_request_state": strings.ToLower(p.State), "other": brief(v.Other)}
	failed := []map[string]any{}
	var stands map[string]bool
	if len(v.Failed) > 0 {
		stands = g.onBase(ctx, p.Base)
	}
	for _, c := range v.Failed {
		entry := map[string]any{"name": c.Name, "url": c.URL, "conclusion": c.Conclusion, "required": c.Required}
		if c.Summary != "" {
			entry["summary"] = c.Summary
		}
		if passed, known := stands[c.Name]; known {
			entry["fails_on_base"] = !passed
		}
		if c.job != 0 {
			if log := g.jobLog(ctx, c.job); log != "" {
				entry["log"] = log
			}
		}
		failed = append(failed, entry)
	}
	out["failed"] = fit(failed)
	return out
}

// fit drops the logs of the last failed checks until the whole list is small
// enough to be a step's output.
func fit(failed []map[string]any) []map[string]any {
	for i := len(failed) - 1; i >= 0; i-- {
		raw, _ := json.Marshal(failed)
		if len(raw) <= maxChecksOutput {
			break
		}
		delete(failed[i], "log")
		delete(failed[i], "summary")
	}
	return failed
}
