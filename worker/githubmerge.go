package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/spec"
)

// Merging a pull request.
//
// Merging writes to the default branch, which nothing else in Weir may do.
// So it is allowed only through a connection whose manager has said so, with
// "allow_merge". The author of a workflow cannot grant it.
//
// Nothing here uses an override of a repository's rules. A rule GitHub
// enforces, such as a required review, refuses the merge, and the handler
// reports that as an answer: it does not fail.

var mergeMethods = map[string]bool{"merge": true, "squash": true, "rebase": true}

// mergeInput is what the merge handlers read from their step's input.
type mergeInput struct {
	checksInput
	// Method is how to merge: merge, squash or rebase. It must be given.
	Method string `json:"method"`
	// HeadSHA, when given, is the only commit that may be merged.
	HeadSHA string `json:"head_sha"`
	// AllowNoChecks lets a pull request with no check that counts be merged.
	AllowNoChecks bool `json:"allow_no_checks"`
}

// mergeRules is what publishing asks of a step that names a merge handler.
func mergeRules(s *spec.Step) []string {
	problems := countRule(s)
	method, fixed := spec.Literal(s.Input["method"])
	switch {
	case s.Input["method"] == nil:
		problems = append(problems, "say how to merge, as method: merge, squash or rebase")
	case fixed && !mergeMethods[method]:
		problems = append(problems, fmt.Sprintf("method %q is not one of merge, squash, rebase", method))
	}
	return problems
}

// mergeable finds the connection for a merge and refuses one that does not
// allow merging, before anything is sent to GitHub.
func mergeable(ctx context.Context, reg *Registry, eng *engine.Engine, client *http.Client, call Call, in *mergeInput) (*github, error) {
	if err := in.check(); err != nil {
		return nil, Permanent(err)
	}
	if !mergeMethods[in.Method] {
		return nil, Permanent(fmt.Errorf("method %q is not one of merge, squash, rebase", in.Method))
	}
	g, err := githubFor(ctx, reg, eng, client, call.RunID, in.Connection, in.Repo)
	if err != nil {
		return nil, err
	}
	if !g.to.AllowMerge {
		return nil, Permanent(fmt.Errorf("connection %s does not allow merging: whoever manages it must set allow_merge", g.to.Connection))
	}
	g.needs = GitHubMerge
	return g, nil
}

// refusal says why a pull request is not to be merged, or nothing when it is.
func refusal(p *pull, v verdict, in *mergeInput) string {
	switch {
	case p.State == "MERGED":
		return "the pull request was already merged"
	case p.State != "OPEN":
		return "the pull request is closed"
	case in.HeadSHA != "" && !strings.EqualFold(in.HeadSHA, p.Head):
		return "the branch moved: its head is " + p.Head + ", not the commit that was checked"
	case len(v.Failed) > 0:
		return "checks failed: " + strings.Join(checkNames(v.Failed), ", ")
	case len(v.Unfinished) > 0:
		return "checks have not finished: " + strings.Join(v.Unfinished, ", ")
	case len(v.Counted) == 0 && !in.AllowNoChecks:
		return "no check counts for this pull request, and the step does not set allow_no_checks"
	}
	return ""
}

// RegisterGitHubMerge adds "github.merge_pull_request" and
// "github.enable_auto_merge", the two deterministic handlers that merge.
func RegisterGitHubMerge(reg *Registry, eng *engine.Engine, client *http.Client) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: secrets.SameHost}
	}
	reg.RegisterRules("github.merge_pull_request", mergeRules)
	reg.RegisterRules("github.enable_auto_merge", mergeRules)

	// Merges now, at the commit that was checked, if every check that
	// counts passed.
	reg.Register("github.merge_pull_request", func(ctx context.Context, call Call) (json.RawMessage, error) {
		var in mergeInput
		if err := json.Unmarshal(call.Input, &in); err != nil {
			return nil, Permanent(fmt.Errorf("the step's input could not be read: %w", err))
		}
		g, err := mergeable(ctx, reg, eng, client, call, &in)
		if err != nil {
			return nil, err
		}
		p, _, err := g.pullChecks(ctx, in.PullRequest)
		if err != nil {
			return nil, err
		}
		v := judge(p, in.Count, in.Checks)
		// What was seen is kept with the step, whatever the answer.
		out := map[string]any{"merged": false, "head_sha": p.Head, "method": in.Method, "checks": brief(v.Counted)}
		if reason := refusal(p, v, &in); reason != "" {
			out["reason"] = reason
			if p.MergeCommit != "" {
				out["commit"] = p.MergeCommit
			}
			return json.Marshal(out)
		}
		var merged struct {
			SHA    string `json:"sha"`
			Merged bool   `json:"merged"`
		}
		status, err := g.do(ctx, http.MethodPut, fmt.Sprintf("/pulls/%d/merge", in.PullRequest),
			map[string]string{"merge_method": in.Method, "sha": p.Head}, &merged)
		switch {
		case err == nil && merged.Merged:
			out["merged"], out["commit"] = true, merged.SHA
		case err == nil:
			out["reason"] = "GitHub did not merge the pull request"
		case status == http.StatusForbidden || status == http.StatusMethodNotAllowed ||
			status == http.StatusConflict || status == http.StatusUnprocessableEntity:
			// GitHub's own refusal: a rule, a review that is missing, a
			// branch that moved. It is an answer, not a fault.
			out["reason"] = err.Error()
		default:
			return nil, err
		}
		return json.Marshal(out)
	})

	// Hands the merge to GitHub, which makes it once every rule of the base
	// branch is met.
	reg.Register("github.enable_auto_merge", func(ctx context.Context, call Call) (json.RawMessage, error) {
		var in mergeInput
		if err := json.Unmarshal(call.Input, &in); err != nil {
			return nil, Permanent(fmt.Errorf("the step's input could not be read: %w", err))
		}
		g, err := mergeable(ctx, reg, eng, client, call, &in)
		if err != nil {
			return nil, err
		}
		owner, name, _ := strings.Cut(g.repo, "/")
		var found struct {
			Repository struct {
				PullRequest *struct {
					ID string `json:"id"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		_, problems, err := g.graphql(ctx, `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){id}}}`,
			map[string]any{"owner": owner, "name": name, "number": in.PullRequest}, &found)
		if err != nil {
			return nil, err
		}
		if len(problems) > 0 || found.Repository.PullRequest == nil {
			return nil, Permanent(errors.New(strings.TrimSpace(fmt.Sprintf("%s has no pull request %d that can be read. %s",
				g.repo, in.PullRequest, strings.Join(problems, "; ")))))
		}
		_, problems, err = g.graphql(ctx, `mutation($id:ID!,$method:PullRequestMergeMethod!){enablePullRequestAutoMerge(input:{pullRequestId:$id,mergeMethod:$method}){clientMutationId}}`,
			map[string]any{"id": found.Repository.PullRequest.ID, "method": strings.ToUpper(in.Method)}, nil)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"enabled": len(problems) == 0, "method": in.Method}
		if len(problems) > 0 {
			out["reason"] = strings.Join(problems, "; ")
		}
		return json.Marshal(out)
	})
}
