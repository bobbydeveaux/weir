package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CloudRun starts each agent as one run of a Cloud Run job, and waits for it
// to end.
//
// The job is made once, by whoever runs Weir: its image, the service account
// it runs as, and the model's credential from Secret Manager. Weir only runs
// it, adding its own address and the one-time token for the step. Weir never
// sees the model's credential, and the job's service account needs no leave
// to do anything in Weir's project but call the model.
//
// Weir's own service account needs to run that one job with overrides, and
// to read and cancel its runs.
type CloudRun struct {
	// Project, Region and Job name the job. Project and Region default to
	// those Weir itself runs in.
	Project, Region, Job string

	// API is Cloud Run's address, and Metadata the address of the machine's
	// metadata server. Empty means Google's own.
	API, Metadata string
	// HTTP makes the calls. Nil means a client with a timeout.
	HTTP *http.Client
	// Poll is how often a run is checked. Zero means every three seconds.
	Poll time.Duration

	mu      sync.Mutex
	token   string
	expires time.Time
}

// Describe implements Launcher.
func (c *CloudRun) Describe() string { return "a run of the Cloud Run job " + c.Job + " each" }

func (c *CloudRun) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// metadata asks the machine's metadata server for one value. It is how a
// service on Google Cloud learns who and where it is, with no key to hold.
func (c *CloudRun) metadata(ctx context.Context, path string) ([]byte, error) {
	base := c.Metadata
	if base == "" {
		base = "http://metadata.google.internal"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/computeMetadata/v1/"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Weir is not running on Google Cloud, or its metadata server could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the metadata server said %d for %s", resp.StatusCode, path)
	}
	return raw, nil
}

// access returns a token for Weir's own service account, asking for a new
// one shortly before the old one runs out. It is held in memory only.
func (c *CloudRun) access(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	raw, err := c.metadata(ctx, "instance/service-accounts/default/token")
	if err != nil {
		return "", err
	}
	var got struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || got.AccessToken == "" {
		return "", errors.New("the metadata server gave no token")
	}
	c.token, c.expires = got.AccessToken, time.Now().Add(time.Duration(got.ExpiresIn)*time.Second-time.Minute)
	return c.token, nil
}

// where fills in the project and region from where Weir itself runs.
func (c *CloudRun) where(ctx context.Context) (project, region string, err error) {
	project, region = c.Project, c.Region
	if project == "" {
		raw, err := c.metadata(ctx, "project/project-id")
		if err != nil {
			return "", "", err
		}
		project = strings.TrimSpace(string(raw))
	}
	if region == "" {
		// "projects/123456/regions/europe-west1"
		raw, err := c.metadata(ctx, "instance/region")
		if err != nil {
			return "", "", err
		}
		parts := strings.Split(strings.TrimSpace(string(raw)), "/")
		region = parts[len(parts)-1]
	}
	return project, region, nil
}

// call makes one request to Cloud Run's API as Weir.
func (c *CloudRun) call(ctx context.Context, method, path string, body, out any) error {
	token, err := c.access(ctx)
	if err != nil {
		return err
	}
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	base := c.API
	if base == "" {
		base = "https://run.googleapis.com"
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+"/v2/"+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("Cloud Run could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error.Message == "" {
			e.Error.Message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("Cloud Run said %d: %s", resp.StatusCode, e.Error.Message)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Launch implements Launcher.
func (c *CloudRun) Launch(ctx context.Context, start AgentStart) error {
	project, region, err := c.where(ctx)
	if err != nil {
		return err
	}
	// This run of the job is given where Weir is and the token for its step.
	// Everything else about it was set when the job was made.
	overrides := map[string]any{"taskCount": 1, "containerOverrides": []map[string]any{{
		"env": []map[string]string{{"name": "WEIR_URL", "value": start.URL}, {"name": "WEIR_AGENT_TOKEN", "value": start.Token}},
	}}}
	if deadline, ok := ctx.Deadline(); ok {
		if left := int(time.Until(deadline).Seconds()); left > 0 {
			overrides["timeout"] = fmt.Sprintf("%ds", left) // Cloud Run stops it too
		}
	}
	var started struct {
		Metadata struct {
			Name string `json:"name"` // projects/…/locations/…/jobs/…/executions/…
		} `json:"metadata"`
	}
	job := fmt.Sprintf("projects/%s/locations/%s/jobs/%s", project, region, c.Job)
	if err := c.call(ctx, http.MethodPost, job+":run", map[string]any{"overrides": overrides}, &started); err != nil {
		return fmt.Errorf("start the agent's job: %w", err)
	}
	if !strings.HasPrefix(started.Metadata.Name, job+"/executions/") {
		return errors.New("Cloud Run started the job but did not say which run it was")
	}
	return c.wait(ctx, started.Metadata.Name)
}

// wait checks one run of the job until it has ended. If the step runs out of
// time or is cancelled first, the run is cancelled too.
func (c *CloudRun) wait(ctx context.Context, run string) error {
	every := c.Poll
	if every == 0 {
		every = 3 * time.Second
	}
	stop := func() error {
		done, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		_ = c.call(done, http.MethodPost, run+":cancel", map[string]any{}, nil)
		return fmt.Errorf("stopped waiting for the agent's job: %w", ctx.Err())
	}
	for {
		var state struct {
			Succeeded  int    `json:"succeededCount"`
			Failed     int    `json:"failedCount"`
			Cancelled  int    `json:"cancelledCount"`
			LogURI     string `json:"logUri"`
			Conditions []struct {
				Type, State, Message string
			} `json:"conditions"`
		}
		if err := c.call(ctx, http.MethodGet, run, nil, &state); err != nil {
			if ctx.Err() != nil {
				return stop()
			}
			return err
		}
		switch {
		case state.Succeeded > 0:
			return nil
		case state.Failed > 0 || state.Cancelled > 0:
			why := "the agent's job failed"
			for _, cond := range state.Conditions {
				if cond.State == "CONDITION_FAILED" && cond.Message != "" {
					why += ": " + cond.Message
					break
				}
			}
			if state.LogURI != "" {
				why += ". Its log is at " + state.LogURI
			}
			return errors.New(why)
		}
		select {
		case <-ctx.Done():
			return stop()
		case <-time.After(every):
		}
	}
}
