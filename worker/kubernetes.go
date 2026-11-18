package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Kubernetes starts each agent as a Kubernetes Job, and waits for it to end.
//
// The Job's pod is given Weir's address and the one-time token for its step.
// It gets no Kubernetes credential of its own, and the model's credential
// comes from a Secret that whoever runs Weir names: Weir never reads it.
//
// It talks to the Kubernetes API directly, as the service account Weir's own
// pod runs as. That account needs to create, read and delete Jobs, and to
// read pods and their logs, in the namespace agents run in.
type Kubernetes struct {
	// Image is what the agent runs in.
	Image string
	// Namespace is where Jobs are made. Empty means Weir's own.
	Namespace string
	// Secret names a Kubernetes Secret whose keys become the pod's
	// environment: the model's credential. Empty means none.
	Secret string
	// ServiceAccount is the account the pod runs as, for a cloud's workload
	// identity. Empty means the namespace's default. Its token is not mounted
	// either way.
	ServiceAccount string
	// CPU and Memory are the pod's requests and limits, such as "1" and
	// "2Gi". Empty leaves them to the cluster.
	CPU, Memory string

	// API is the Kubernetes API's address, and Token what Weir presents to
	// it. Empty means those of the pod Weir runs in.
	API, Token string
	// HTTP makes the calls. Nil means a client that trusts the cluster's own
	// certificate.
	HTTP *http.Client
	// Poll is how often a Job is checked. Zero means every two seconds.
	Poll time.Duration
}

// inCluster is where a pod finds its service account.
const inCluster = "/var/run/secrets/kubernetes.io/serviceaccount/"

// Describe implements Launcher.
func (k *Kubernetes) Describe() string { return "a Kubernetes Job each, from the image " + k.Image }

// connect fills in the API's address, the token and the namespace from the
// pod Weir runs in, for whatever was not set.
func (k *Kubernetes) connect() (api, token, namespace string, client *http.Client, err error) {
	api, token, namespace, client = k.API, k.Token, k.Namespace, k.HTTP
	if api == "" {
		host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
		if host == "" {
			return "", "", "", nil, errors.New("Weir is not running in a Kubernetes cluster, so it cannot start an agent as a Job")
		}
		api = "https://" + host + ":" + port
		raw, err := os.ReadFile(inCluster + "token")
		if err != nil {
			return "", "", "", nil, fmt.Errorf("read Weir's own service account token: %w", err)
		}
		token = strings.TrimSpace(string(raw))
	}
	if namespace == "" {
		raw, err := os.ReadFile(inCluster + "namespace")
		if err != nil {
			return "", "", "", nil, errors.New("say which namespace agents run in: Weir could not tell which one it is in")
		}
		namespace = strings.TrimSpace(string(raw))
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
		if ca, err := os.ReadFile(inCluster + "ca.crt"); err == nil {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(ca)
			client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
		}
	}
	return strings.TrimRight(api, "/"), token, namespace, client, nil
}

// kube is a connection to one namespace of the Kubernetes API.
type kube struct {
	api, token, namespace string
	client                *http.Client
}

// call makes one request and decodes the reply into out. A reply that is not
// a success is an error that says what Kubernetes said.
func (k *kube) call(ctx context.Context, method, path string, body, out any) (int, error) {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.api+path, payload)
	if err != nil {
		return 0, err
	}
	if k.token != "" {
		req.Header.Set("Authorization", "Bearer "+k.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := k.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Kubernetes could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var status struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &status)
		if status.Message == "" {
			status.Message = http.StatusText(resp.StatusCode)
		}
		return resp.StatusCode, fmt.Errorf("Kubernetes said %d: %s", resp.StatusCode, status.Message)
	}
	if text, ok := out.(*string); ok {
		*text = string(raw)
		return resp.StatusCode, nil
	}
	if out != nil {
		return resp.StatusCode, json.Unmarshal(raw, out)
	}
	return resp.StatusCode, nil
}

// job is the Job for one agent. The pod is told where Weir is and given the
// token for its step, and nothing else of Weir's.
func (k *Kubernetes) job(name string, start AgentStart, deadline time.Time) map[string]any {
	container := map[string]any{
		"name": "agent", "image": k.Image, "command": []string{"weir", "agent"},
		"env": []map[string]string{{"name": "WEIR_URL", "value": start.URL}, {"name": "WEIR_AGENT_TOKEN", "value": start.Token}},
		"securityContext": map[string]any{"allowPrivilegeEscalation": false, "runAsNonRoot": true,
			"capabilities": map[string]any{"drop": []string{"ALL"}}},
	}
	if k.Secret != "" {
		container["envFrom"] = []map[string]any{{"secretRef": map[string]string{"name": k.Secret}}}
	}
	amounts := map[string]string{}
	if k.CPU != "" {
		amounts["cpu"] = k.CPU
	}
	if k.Memory != "" {
		amounts["memory"] = k.Memory
	}
	if len(amounts) > 0 {
		container["resources"] = map[string]any{"requests": amounts, "limits": amounts}
	}
	pod := map[string]any{
		"restartPolicy": "Never",
		// The agent has no business with the cluster it runs in.
		"automountServiceAccountToken": false,
		"containers":                   []any{container},
	}
	if k.ServiceAccount != "" {
		pod["serviceAccountName"] = k.ServiceAccount
	}
	spec := map[string]any{
		"backoffLimit":            0,   // an agent is run once; Weir decides whether to try again
		"ttlSecondsAfterFinished": 600, // a Job that Weir could not remove still goes
		"template": map[string]any{
			"metadata": map[string]any{"labels": map[string]string{"app.kubernetes.io/name": "weir-agent"}},
			"spec":     pod,
		},
	}
	if !deadline.IsZero() {
		if left := int(time.Until(deadline).Seconds()); left > 0 {
			spec["activeDeadlineSeconds"] = left
		}
	}
	return map[string]any{
		"apiVersion": "batch/v1", "kind": "Job",
		"metadata": map[string]any{"name": name, "labels": map[string]string{"app.kubernetes.io/name": "weir-agent", "app.kubernetes.io/managed-by": "weir"}},
		"spec":     spec,
	}
}

// Launch implements Launcher.
func (k *Kubernetes) Launch(ctx context.Context, start AgentStart) error {
	api, token, namespace, client, err := k.connect()
	if err != nil {
		return err
	}
	c := &kube{api: api, token: token, namespace: namespace, client: client}
	// The name is made from the token without giving it away.
	sum := sha256.Sum256([]byte(start.Token))
	name := "weir-agent-" + hex.EncodeToString(sum[:8])
	jobs := "/apis/batch/v1/namespaces/" + namespace + "/jobs"
	deadline, _ := ctx.Deadline()
	if _, err := c.call(ctx, http.MethodPost, jobs, k.job(name, start, deadline), nil); err != nil {
		return fmt.Errorf("start the agent's Job: %w", err)
	}
	// Whatever happens, the Job is removed, and its pod with it.
	defer func() {
		done, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		_, _ = c.call(done, http.MethodDelete, jobs+"/"+name+"?propagationPolicy=Background", nil, nil)
	}()
	return k.wait(ctx, c, jobs+"/"+name, name)
}

// wait checks the Job until it has succeeded or failed.
func (k *Kubernetes) wait(ctx context.Context, c *kube, path, name string) error {
	every := k.Poll
	if every == 0 {
		every = 2 * time.Second
	}
	for {
		var job struct {
			Status struct {
				Succeeded  int `json:"succeeded"`
				Failed     int `json:"failed"`
				Conditions []struct {
					Type, Status, Reason, Message string
				} `json:"conditions"`
			} `json:"status"`
		}
		if _, err := c.call(ctx, http.MethodGet, path, nil, &job); err != nil {
			return err
		}
		if job.Status.Succeeded > 0 {
			return nil
		}
		for _, cond := range job.Status.Conditions {
			if cond.Type == "Failed" && cond.Status == "True" {
				return fmt.Errorf("the agent's Job failed (%s): %s%s", cond.Reason, cond.Message, c.lastWords(ctx, name))
			}
		}
		if job.Status.Failed > 0 {
			return fmt.Errorf("the agent's Job failed%s", c.lastWords(ctx, name))
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("stopped waiting for the agent's Job: %w", ctx.Err())
		case <-time.After(every):
		}
	}
}

// lastWords returns the end of the log of a Job's pod, for an error. It is
// empty if the log cannot be read.
func (k *kube) lastWords(ctx context.Context, job string) string {
	read, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var pods struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	base := "/api/v1/namespaces/" + k.namespace + "/pods"
	if _, err := k.call(read, http.MethodGet, base+"?labelSelector=job-name%3D"+job, nil, &pods); err != nil || len(pods.Items) == 0 {
		return ""
	}
	var log string
	if _, err := k.call(read, http.MethodGet, base+"/"+pods.Items[0].Metadata.Name+"/log?tailLines=15", nil, &log); err != nil {
		return ""
	}
	if log = strings.TrimSpace(log); log == "" {
		return ""
	}
	if len(log) > 800 {
		log = log[len(log)-800:]
	}
	return ". Its log ended: " + log
}
