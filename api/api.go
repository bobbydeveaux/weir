// Package api is Weir's HTTP interface.
//
// The caller's identity comes from the X-Weir-Actor header and is not yet
// authenticated. Run the API only where every caller is trusted until API
// keys are added.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/bobbydeveaux/weir/engine"
)

// maxBody is the largest request body the API reads.
const maxBody = 1 << 20

// New returns the API's handler.
func New(eng *engine.Engine) http.Handler {
	s := &server{eng: eng}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/definitions", s.publish)
	mux.HandleFunc("POST /v1/templates/{key}", s.template)
	mux.HandleFunc("PUT /v1/connections/{name}", s.connection)
	mux.HandleFunc("POST /v1/runs", s.start)
	mux.HandleFunc("GET /v1/runs/{id}", s.run)
	mux.HandleFunc("POST /v1/runs/{id}/resume", s.resume)
	mux.HandleFunc("GET /v1/tasks", s.tasks)
	mux.HandleFunc("POST /v1/tasks/{id}/decision", s.decide)
	mux.HandleFunc("POST /v1/callbacks/{token}", s.callback)
	return mux
}

type server struct{ eng *engine.Engine }

func actor(r *http.Request) string { return r.Header.Get("X-Weir-Actor") }

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// fail maps an engine error to a status code.
func fail(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, engine.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, engine.ErrNotAllowed):
		status = http.StatusForbidden
	case errors.Is(err, engine.ErrStaleToken), errors.Is(err, engine.ErrTaskClosed), errors.Is(err, engine.ErrNotBlocked):
		status = http.StatusConflict
	}
	reply(w, status, map[string]string{"error": err.Error()})
}

func body(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		reply(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "the request body is too large"})
		return nil, false
	}
	return raw, true
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	raw, ok := body(w, r)
	if !ok {
		return false
	}
	if len(raw) == 0 {
		return true
	}
	if err := json.Unmarshal(raw, into); err != nil {
		reply(w, http.StatusBadRequest, map[string]string{"error": "the request body is not valid JSON"})
		return false
	}
	return true
}

func needActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	a := actor(r)
	if a == "" {
		reply(w, http.StatusBadRequest, map[string]string{"error": "set the X-Weir-Actor header to say who is asking"})
		return "", false
	}
	return a, true
}

func (s *server) publish(w http.ResponseWriter, r *http.Request) {
	who, ok := needActor(w, r)
	if !ok {
		return
	}
	source, ok := body(w, r)
	if !ok {
		return
	}
	out, err := s.eng.Publish(r.Context(), source, who)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusCreated, map[string]any{"namespace": out.Namespace, "name": out.Name, "version": out.Version,
		"not_yet_enforced": out.Unsupported})
}

func (s *server) template(w http.ResponseWriter, r *http.Request) {
	who, ok := needActor(w, r)
	if !ok {
		return
	}
	var t engine.Template
	if !decode(w, r, &t) {
		return
	}
	if t.User == "" {
		reply(w, http.StatusBadRequest, map[string]string{"error": "a template needs a user prompt"})
		return
	}
	version, err := s.eng.PublishTemplate(r.Context(), r.PathValue("key"), t, who)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusCreated, map[string]any{"key": r.PathValue("key"), "version": version})
}

func (s *server) connection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BaseURL string `json:"base_url"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.BaseURL == "" {
		reply(w, http.StatusBadRequest, map[string]string{"error": "a connection needs a base_url"})
		return
	}
	if err := s.eng.PutConnection(r.Context(), r.PathValue("name"), in.BaseURL); err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"name": r.PathValue("name")})
}

func (s *server) start(w http.ResponseWriter, r *http.Request) {
	who, ok := needActor(w, r)
	if !ok {
		return
	}
	var in struct {
		Namespace      string          `json:"namespace"`
		Name           string          `json:"name"`
		Alias          string          `json:"alias"`
		Input          json.RawMessage `json:"input"`
		IdempotencyKey string          `json:"idempotency_key"`
	}
	if !decode(w, r, &in) {
		return
	}
	id, created, err := s.eng.StartRun(r.Context(), engine.StartRequest{Namespace: in.Namespace, Name: in.Name, Alias: in.Alias,
		Input: in.Input, Requester: who, IdempotencyKey: in.IdempotencyKey})
	if err != nil {
		fail(w, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	reply(w, status, map[string]any{"id": id, "created": created})
}

func (s *server) run(w http.ResponseWriter, r *http.Request) {
	v, err := s.eng.Run(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, v)
}

func (s *server) resume(w http.ResponseWriter, r *http.Request) {
	who, ok := needActor(w, r)
	if !ok {
		return
	}
	var in struct {
		BudgetUSD float64 `json:"budget_usd"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.eng.Resume(r.Context(), r.PathValue("id"), who, in.BudgetUSD); err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (s *server) tasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.eng.Tasks(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	if tasks == nil {
		tasks = []engine.Task{}
	}
	reply(w, http.StatusOK, tasks)
}

func (s *server) decide(w http.ResponseWriter, r *http.Request) {
	who, ok := needActor(w, r)
	if !ok {
		return
	}
	var in struct {
		Outcome string `json:"outcome"`
		Note    string `json:"note"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.eng.Decide(r.Context(), r.PathValue("id"), who, in.Outcome, in.Note); err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"status": "decided"})
}

// callback is where a third party returns a token. The token is the
// credential: whoever holds the live token for an attempt may complete it, once.
func (s *server) callback(w http.ResponseWriter, r *http.Request) {
	output, ok := body(w, r)
	if !ok {
		return
	}
	if len(output) > 0 && !json.Valid(output) {
		reply(w, http.StatusBadRequest, map[string]string{"error": "the callback body is not valid JSON"})
		return
	}
	if err := s.eng.Complete(r.Context(), r.PathValue("token"), engine.Result{Output: output, Actor: "callback"}); err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"status": "accepted"})
}
