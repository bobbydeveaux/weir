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

// fakeCluster is enough of the Kubernetes API for the launcher: Jobs, pods
// and a pod's log, in one namespace.
type fakeCluster struct {
	mu      sync.Mutex
	srv     *httptest.Server
	created map[string]any
	deleted []string
	polls   int
	// outcome is what a Job reports once it has been polled twice.
	outcome string
	auth    string
}

func newFakeCluster(t *testing.T, outcome string) *fakeCluster {
	f := &fakeCluster{outcome: outcome}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == "/apis/batch/v1/namespaces/agents/jobs" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&f.created)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
		case strings.HasPrefix(path, "/apis/batch/v1/namespaces/agents/jobs/") && r.Method == http.MethodDelete:
			f.deleted = append(f.deleted, strings.TrimPrefix(path, "/apis/batch/v1/namespaces/agents/jobs/")+"?"+r.URL.RawQuery)
			_, _ = io.WriteString(w, `{}`)
		case strings.HasPrefix(path, "/apis/batch/v1/namespaces/agents/jobs/"):
			f.polls++
			_, _ = io.WriteString(w, f.status())
		case path == "/api/v1/namespaces/agents/pods":
			_, _ = io.WriteString(w, `{"items": [{"metadata": {"name": "weir-agent-pod"}}]}`)
		case path == "/api/v1/namespaces/agents/pods/weir-agent-pod/log":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "weir: Weir could not be reached at http://weir:8080\n")
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message": "not found"}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCluster) status() string {
	switch {
	case f.polls < 2 || f.outcome == "never":
		return `{"status": {"active": 1}}`
	case f.outcome == "succeeded":
		return `{"status": {"succeeded": 1}}`
	}
	return `{"status": {"failed": 1, "conditions": [{"type": "Failed", "status": "True", "reason": "BackoffLimitExceeded", "message": "Job has reached the specified backoff limit"}]}}`
}

func TestAnAgentIsStartedAsAKubernetesJob(t *testing.T) {
	cluster := newFakeCluster(t, "succeeded")
	k := &Kubernetes{Image: "weir-agent:1", Namespace: "agents", Secret: "model-credential", ServiceAccount: "agent",
		CPU: "1", Memory: "2Gi", API: cluster.srv.URL, Token: "weirs-own-token", HTTP: cluster.srv.Client(), Poll: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if err := k.Launch(ctx, AgentStart{URL: "http://weir.weir.svc:8080", Token: "one-time-token-for-this-step"}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if cluster.auth != "Bearer weirs-own-token" {
		t.Errorf("Weir presented %q to Kubernetes", cluster.auth)
	}
	raw, _ := json.Marshal(cluster.created)
	manifest := string(raw)
	for _, want := range []string{
		`"image":"weir-agent:1"`, `"command":["weir","agent"]`,
		`{"name":"WEIR_URL","value":"http://weir.weir.svc:8080"}`, `{"name":"WEIR_AGENT_TOKEN","value":"one-time-token-for-this-step"}`,
		`"envFrom":[{"secretRef":{"name":"model-credential"}}]`, // the credential comes from a Secret, by name
		`"automountServiceAccountToken":false`,                  // the agent gets no credential for the cluster
		`"runAsNonRoot":true`, `"allowPrivilegeEscalation":false`, `"drop":["ALL"]`,
		`"serviceAccountName":"agent"`, `"limits":{"cpu":"1","memory":"2Gi"}`,
		`"backoffLimit":0`, `"restartPolicy":"Never"`, `"ttlSecondsAfterFinished":600`,
	} {
		if !strings.Contains(manifest, want) {
			t.Errorf("the Job lacks %s:\n%s", want, manifest)
		}
	}
	// The Job is given the time the step has left, so the cluster stops it too.
	if left, _ := cluster.created["spec"].(map[string]any)["activeDeadlineSeconds"].(float64); left < 3500 || left > 3600 {
		t.Errorf("activeDeadlineSeconds = %v, want about an hour", left)
	}
	name := cluster.created["metadata"].(map[string]any)["name"].(string)
	if !strings.HasPrefix(name, "weir-agent-") || strings.Contains(name, "one-time") {
		t.Errorf("the Job is named %q", name)
	}
	// The Job is removed when it ends, and its pod with it.
	if len(cluster.deleted) != 1 || cluster.deleted[0] != name+"?propagationPolicy=Background" {
		t.Errorf("deleted = %v", cluster.deleted)
	}

	// A Job that fails says why, with the end of its pod's log.
	failed := newFakeCluster(t, "failed")
	k.API, k.HTTP = failed.srv.URL, failed.srv.Client()
	err := k.Launch(context.Background(), AgentStart{URL: "http://weir:8080", Token: "t2"})
	if err == nil || !strings.Contains(err.Error(), "BackoffLimitExceeded") || !strings.Contains(err.Error(), "Weir could not be reached") {
		t.Errorf("a failed Job = %v", err)
	}
	if len(failed.deleted) != 1 {
		t.Errorf("a failed Job was not removed: %v", failed.deleted)
	}

	// A step that runs out of time stops waiting, and still removes the Job.
	stuck := newFakeCluster(t, "never")
	k.API, k.HTTP = stuck.srv.URL, stuck.srv.Client()
	short, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	if err := k.Launch(short, AgentStart{URL: "http://weir:8080", Token: "t3"}); err == nil || !strings.Contains(err.Error(), "stopped waiting") {
		t.Errorf("a Job that never ends = %v", err)
	}
	if len(stuck.deleted) != 1 {
		t.Errorf("a Job that never ended was not removed: %v", stuck.deleted)
	}

	// Outside a cluster, with nothing set, it says so.
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	if err := (&Kubernetes{Image: "x"}).Launch(context.Background(), AgentStart{}); err == nil || !strings.Contains(err.Error(), "not running in a Kubernetes cluster") {
		t.Errorf("outside a cluster = %v", err)
	}
}
