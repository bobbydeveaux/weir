package worker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGoogle stands in for the metadata server and Cloud Run's API.
type fakeGoogle struct {
	mu        sync.Mutex
	srv       *httptest.Server
	ran       map[string]any // the body of the request that ran the job
	ranPath   string
	tokens    int // times a token was asked for
	polls     int
	cancelled int
	outcome   string
}

func newFakeGoogle(t *testing.T, outcome string) *fakeGoogle {
	f := &fakeGoogle{outcome: outcome}
	const run = "projects/hackspace/locations/europe-west1/jobs/weir-agent/executions/weir-agent-abc"
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		path := r.URL.Path
		if strings.HasPrefix(path, "/computeMetadata/v1/") {
			if r.Header.Get("Metadata-Flavor") != "Google" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			switch strings.TrimPrefix(path, "/computeMetadata/v1/") {
			case "instance/service-accounts/default/token":
				f.tokens++
				_, _ = io.WriteString(w, `{"access_token": "weirs-own-token", "expires_in": 3600}`)
			case "project/project-id":
				_, _ = io.WriteString(w, "hackspace")
			case "instance/region":
				_, _ = io.WriteString(w, "projects/123456/regions/europe-west1")
			}
			return
		}
		if r.Header.Get("Authorization") != "Bearer weirs-own-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error": {"message": "no token"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case path == "/v2/projects/hackspace/locations/europe-west1/jobs/weir-agent:run":
			f.ranPath = path
			_ = json.NewDecoder(r.Body).Decode(&f.ran)
			_, _ = io.WriteString(w, `{"name": "projects/hackspace/locations/europe-west1/operations/op1", "metadata": {"name": "`+run+`"}}`)
		case path == "/v2/"+run+":cancel":
			f.cancelled++
			_, _ = io.WriteString(w, `{}`)
		case path == "/v2/"+run:
			f.polls++
			_, _ = io.WriteString(w, f.state())
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error": {"message": "Resource not found"}}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGoogle) state() string {
	switch {
	case f.polls < 2 || f.outcome == "never":
		return `{"runningCount": 1}`
	case f.outcome == "succeeded":
		return `{"succeededCount": 1}`
	}
	return `{"failedCount": 1, "logUri": "https://console.cloud.google.com/logs/x", "conditions": [{"type": "Completed", "state": "CONDITION_FAILED", "message": "Task weir-agent-abc-task0 failed with exit code 1"}]}`
}

func TestAnAgentIsStartedAsARunOfACloudRunJob(t *testing.T) {
	google := newFakeGoogle(t, "succeeded")
	c := &CloudRun{Job: "weir-agent", API: google.srv.URL, Metadata: google.srv.URL, Poll: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if err := c.Launch(ctx, AgentStart{URL: "https://weir.example.com", Token: "one-time-token"}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	// The project and region are Weir's own, learnt from the machine it runs on.
	if google.ranPath != "/v2/projects/hackspace/locations/europe-west1/jobs/weir-agent:run" {
		t.Errorf("ran %s", google.ranPath)
	}
	raw, _ := json.Marshal(google.ran)
	for _, want := range []string{`{"name":"WEIR_URL","value":"https://weir.example.com"}`, `{"name":"WEIR_AGENT_TOKEN","value":"one-time-token"}`, `"taskCount":1`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the run lacks %s:\n%s", want, raw)
		}
	}
	// Nothing about the image, the account or the model's credential is sent:
	// those belong to the job, which Weir did not make.
	for _, absent := range []string{"image", "serviceAccount", "secret"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(absent)) {
			t.Errorf("the run sets %s:\n%s", absent, raw)
		}
	}
	// The run is given the time the step has left, so Cloud Run stops it too.
	timeout, _ := google.ran["overrides"].(map[string]any)["timeout"].(string)
	if seconds, err := time.ParseDuration(timeout); err != nil || seconds < 58*time.Minute || seconds > time.Hour {
		t.Errorf("timeout = %q, want about an hour", timeout)
	}
	// A second agent reuses the token Weir already holds.
	if err := c.Launch(ctx, AgentStart{URL: "https://weir.example.com", Token: "t2"}); err != nil || google.tokens != 1 {
		t.Errorf("a second launch: %v, and %d tokens were asked for", err, google.tokens)
	}

	// A run that fails says why, and where its log is.
	failed := newFakeGoogle(t, "failed")
	c = &CloudRun{Job: "weir-agent", API: failed.srv.URL, Metadata: failed.srv.URL, Poll: time.Millisecond}
	err := c.Launch(context.Background(), AgentStart{URL: "u", Token: "t"})
	if err == nil || !strings.Contains(err.Error(), "failed with exit code 1") || !strings.Contains(err.Error(), "console.cloud.google.com/logs") {
		t.Errorf("a failed run = %v", err)
	}

	// A step that runs out of time cancels the run.
	stuck := newFakeGoogle(t, "never")
	c = &CloudRun{Job: "weir-agent", API: stuck.srv.URL, Metadata: stuck.srv.URL, Poll: time.Millisecond}
	short, stop := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer stop()
	if err := c.Launch(short, AgentStart{URL: "u", Token: "t"}); err == nil || !strings.Contains(err.Error(), "stopped waiting") {
		t.Errorf("a run that never ends = %v", err)
	}
	if stuck.cancelled != 1 {
		t.Errorf("a run that never ended was cancelled %d times", stuck.cancelled)
	}

	// A job that does not exist says so.
	c = &CloudRun{Job: "no-such-job", API: google.srv.URL, Metadata: google.srv.URL}
	if err := c.Launch(context.Background(), AgentStart{}); err == nil || !strings.Contains(err.Error(), "Resource not found") {
		t.Errorf("a missing job = %v", err)
	}
	// Off Google Cloud, it says so.
	c = &CloudRun{Job: "weir-agent", Metadata: "http://127.0.0.1:1"}
	if err := c.Launch(context.Background(), AgentStart{}); err == nil || !strings.Contains(err.Error(), "not running on Google Cloud") {
		t.Errorf("off Google Cloud = %v", err)
	}
}
