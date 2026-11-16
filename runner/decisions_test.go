package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// decisionServer plays a decision service: it records the request and sends reply.
func decisionServer(t *testing.T, status int, reply string, path *string, header *http.Header, body *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path, *header = r.URL.Path, r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestJevDecides(t *testing.T) {
	var path string
	var header http.Header
	var body map[string]any
	srv := decisionServer(t, 200, `{"model":"jev-1.13.0","answers":{"decision":{"type":"choice","choice":"refund","confidence":0.97,
		"probabilities":{"refund":0.97,"other":0.03}}},"usage":{"input_tokens":1000,"output_tokens":34}}`, &path, &header, &body)
	d := &Decisions{Service: ServiceJev, Key: "k", BaseURL: srv.URL}
	res, err := d.Decide(context.Background(), DecideRequest{System: "Route it.", Prompt: "I want my money back", Labels: []string{"refund", "other"},
		Describe: map[string]string{"refund": "Wants money back"}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if res.Label != "refund" || res.Confidence != 0.97 {
		t.Errorf("answer = %s %.2f", res.Label, res.Confidence)
	}
	if path != "/v1/systemone" || header.Get("Authorization") != "Bearer k" {
		t.Errorf("request went to %s with Authorization %q", path, header.Get("Authorization"))
	}
	if body["state"] != "I want my money back" || body["model"] != "jev-latest" {
		t.Errorf("body = %v", body)
	}
	q := body["questions"].(map[string]any)["decision"].(map[string]any)
	criteria := q["criteria"].(map[string]any)
	if criteria["refund"] != "Wants money back" || criteria["other"] != nil {
		t.Errorf("criteria = %v, want the description sent and the bare label left empty", criteria)
	}
	if q["type"] != "choice" || len(criteria) != 2 || !strings.Contains(q["instructions"].(string), "Route it.") {
		t.Errorf("question = %v", q)
	}
	if res.Usage.Provider != "jev" || res.Usage.Model != "jev-1.13.0" || res.Usage.TokensIn != 1000 {
		t.Errorf("usage = %+v", res.Usage)
	}
	// 1,000 tokens in at $0.042 a million.
	if want := 0.000042; res.Usage.CostUSD < want-1e-12 || res.Usage.CostUSD > want+1e-12 {
		t.Errorf("cost = %v, want %v", res.Usage.CostUSD, want)
	}
}

func TestGeminiDecisionDecides(t *testing.T) {
	var path string
	var header http.Header
	var body map[string]any
	srv := decisionServer(t, 200, `{"answers":{"decision":{"type":"choice","choice":"other","probabilities":{"other":0.6,"refund":0.4},"confidence":0.6}},
		"usage":{"total_input_tokens":100,"input_tokens_by_modality":[{"modality":"text","tokens":100}],"total_output_tokens":0,"total_tokens":100}}`, &path, &header, &body)
	d := &Decisions{Service: ServiceGeminiDecision, Key: "g", BaseURL: srv.URL}
	res, err := d.Decide(context.Background(), DecideRequest{Prompt: "hello", Labels: []string{"refund", "other"}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if res.Label != "other" || res.Confidence != 0.6 || res.Usage.TokensIn != 100 {
		t.Errorf("answer = %s %.2f, usage %+v", res.Label, res.Confidence, res.Usage)
	}
	if path != "/v1beta/decisions" || header.Get("x-goog-api-key") != "g" || header.Get("Authorization") != "" {
		t.Errorf("request went to %s with headers %v", path, header)
	}
	if body["input"] != "hello" || body["model"] != "gemini-decision-1" {
		t.Errorf("body = %v", body)
	}
	if _, has := body["state"]; has {
		t.Error("the Gemini request carried Jev's state field")
	}
}

func TestDecisionServiceErrorsAreReported(t *testing.T) {
	var path string
	var header http.Header
	var body map[string]any
	labels := []string{"a", "b"}

	srv := decisionServer(t, 400, `{"error":{"code":"safety","message":"blocked by policy"}}`, &path, &header, &body)
	_, err := (&Decisions{Service: ServiceGeminiDecision, Key: "g", BaseURL: srv.URL}).Decide(context.Background(), DecideRequest{Prompt: "x", Labels: labels})
	if err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Errorf("err = %v, want Gemini's message", err)
	}
	srv = decisionServer(t, 400, `{"detail":"unknown model"}`, &path, &header, &body)
	_, err = (&Decisions{Service: ServiceJev, Key: "k", BaseURL: srv.URL}).Decide(context.Background(), DecideRequest{Prompt: "x", Labels: labels})
	if err == nil || !strings.Contains(err.Error(), "unknown model") {
		t.Errorf("err = %v, want Jev's message", err)
	}
	if _, err := (&Decisions{Service: ServiceJev}).Decide(context.Background(), DecideRequest{Prompt: "x", Labels: labels}); err == nil {
		t.Error("a call with no key was allowed")
	}
	if _, err := (&Decisions{Service: ServiceJev, Key: "k"}).Run(context.Background(), RunRequest{}); err == nil {
		t.Error("a decision model ran an ai step")
	}
}

func TestRouterSendsADecisionToTheNamedProvider(t *testing.T) {
	answer := func(label string) *Fake {
		return &Fake{Decisions: map[string]DecideResult{"s": {Label: label, Confidence: 1}}}
	}
	r := Router{Default: answer("from-default"), DecideDefault: "jev",
		Deciders: map[string]Runner{"jev": answer("from-jev"), "claude": answer("from-claude"),
			"gemini-decision": NotSetUp{Provider: "gemini-decision", Hint: "set WEIR_GEMINI_API_KEY"}}}

	for provider, want := range map[string]string{"": "from-jev", "claude": "from-claude", "jev": "from-jev"} {
		res, err := r.Decide(context.Background(), DecideRequest{Step: "s", Provider: provider})
		if err != nil || res.Label != want {
			t.Errorf("provider %q answered %q (%v), want %q", provider, res.Label, err, want)
		}
	}
	if _, err := r.Decide(context.Background(), DecideRequest{Step: "s", Provider: "gemini-decision"}); err == nil ||
		!strings.Contains(err.Error(), "WEIR_GEMINI_API_KEY") {
		t.Errorf("err = %v, want a hint about the missing key", err)
	}
	r.DecideDefault = ""
	if res, _ := r.Decide(context.Background(), DecideRequest{Step: "s"}); res.Label != "from-default" {
		t.Errorf("with no default provider the answer was %q", res.Label)
	}
}

// TestDecisionsLive makes one real call to each service that has a key set.
func TestDecisionsLive(t *testing.T) {
	keys := map[string]string{ServiceJev: os.Getenv("WEIR_TYPESAFE_KEY"), ServiceGeminiDecision: os.Getenv("WEIR_GEMINI_API_KEY")}
	if os.Getenv("WEIR_TEST_DECISIONS") == "" {
		t.Skip("set WEIR_TEST_DECISIONS=1 with WEIR_TYPESAFE_KEY or WEIR_GEMINI_API_KEY to make real calls")
	}
	for service, key := range keys {
		if key == "" {
			continue
		}
		t.Run(service, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			res, err := (&Decisions{Service: service, Key: key}).Decide(ctx, DecideRequest{
				System: "You route support messages.",
				Prompt: "Thanks, that fixed it!", Labels: []string{"quick", "considered"},
				Describe: map[string]string{"quick": "Thanks or small talk", "considered": "Needs real work"}})
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			t.Logf("label=%s confidence=%.2f model=%s tokens=%d cost=$%.7f took=%s", res.Label, res.Confidence,
				res.Usage.Model, res.Usage.TokensIn, res.Usage.CostUSD, res.Usage.Duration.Round(time.Millisecond))
			if res.Label != "quick" {
				t.Errorf("label = %q, want quick", res.Label)
			}
		})
	}
}
