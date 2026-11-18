package secrets

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// googleMetadata is where a server on Google Cloud asks for a token that says
// who it is. It is a variable so that a test can stand in for it.
var googleMetadata = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity"

// GoogleIDTokens returns a source of ID tokens for a server running on Google
// Cloud (Cloud Run, GKE, Compute Engine). The platform signs each token; it
// names the server's own service account and the one address it is for, and
// lasts about an hour. A token is kept and used again until it is nearly spent.
func GoogleIDTokens(client *http.Client) func(ctx context.Context, audience string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	type kept struct {
		token string
		until time.Time
	}
	var mu sync.Mutex
	tokens := map[string]kept{}
	return func(ctx context.Context, audience string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if k, ok := tokens[audience]; ok && time.Now().Before(k.until) {
			return k.token, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleMetadata+"?audience="+url.QueryEscape(audience), nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Metadata-Flavor", "Google")
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("the platform could not be asked for an ID token (is this server running on Google Cloud?): %w", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		token := strings.TrimSpace(string(raw))
		if resp.StatusCode != http.StatusOK || token == "" {
			return "", fmt.Errorf("the platform refused an ID token for %s (HTTP %d)", audience, resp.StatusCode)
		}
		tokens[audience] = kept{token: token, until: time.Now().Add(45 * time.Minute)}
		return token, nil
	}
}
