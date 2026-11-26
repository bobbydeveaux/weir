// Package api is Weir's HTTP interface.
//
// By default the API trusts its callers: the workspace and the actor are
// whatever the request says. WithAdminKey turns authentication on, after
// which every request must carry an API key (see auth.go).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"

	"github.com/bobbydeveaux/weir/agent"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/secrets"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/spec"
	"github.com/bobbydeveaux/weir/worker"
)

// maxBody is the largest request body the API reads.
const maxBody = 1 << 20

// New returns the API's handler. reg holds the tools agents may call through
// the tool endpoints; it may be nil, in which case no tools are offered.
func New(eng *engine.Engine, reg *worker.Registry, opts ...Option) http.Handler {
	if reg == nil {
		reg = worker.NewRegistry()
	}
	s := &server{eng: eng, reg: reg}
	for _, opt := range opts {
		opt(s)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/definitions", s.publish)
	mux.HandleFunc("GET /v1/definitions", s.raw(func(r *http.Request) (json.RawMessage, error) { return s.in(r).Definitions(r.Context()) }))
	mux.HandleFunc("GET /v1/definitions/{namespace}/{name}", s.raw(func(r *http.Request) (json.RawMessage, error) {
		version, _ := strconv.Atoi(r.URL.Query().Get("version"))
		return s.in(r).Definition(r.Context(), r.PathValue("namespace"), r.PathValue("name"), version)
	}))
	mux.HandleFunc("GET /v1/templates", s.raw(func(r *http.Request) (json.RawMessage, error) { return s.in(r).Templates(r.Context()) }))
	mux.HandleFunc("GET /v1/templates/{key}", s.raw(func(r *http.Request) (json.RawMessage, error) {
		return s.in(r).TemplateVersions(r.Context(), r.PathValue("key"))
	}))
	mux.HandleFunc("GET /v1/connections", s.raw(func(r *http.Request) (json.RawMessage, error) { return s.in(r).Connections(r.Context()) }))
	mux.HandleFunc("GET /v1/runs", s.raw(func(r *http.Request) (json.RawMessage, error) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		return s.in(r).Runs(r.Context(), engine.RunFilter{Namespace: q.Get("namespace"), Name: q.Get("name"),
			Status: q.Get("status"), Requester: q.Get("requester"), Limit: limit})
	}))
	mux.HandleFunc("GET /v1/runs/{id}/record", s.raw(func(r *http.Request) (json.RawMessage, error) {
		return s.in(r).RunRecord(r.Context(), r.PathValue("id"))
	}))
	mux.HandleFunc("GET /v1/runs/{id}/files", s.raw(func(r *http.Request) (json.RawMessage, error) {
		return s.in(r).RunFiles(r.Context(), r.PathValue("id"))
	}))
	mux.HandleFunc("GET /v1/runs/{id}/files/{step}/{path...}", s.file)
	mux.HandleFunc("GET /v1/usage", s.raw(func(r *http.Request) (json.RawMessage, error) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		return s.in(r).Usage(r.Context(), r.URL.Query().Get("namespace"), days)
	}))
	mux.HandleFunc("POST /v1/events", func(w http.ResponseWriter, r *http.Request) {
		who, ok := needActor(w, r)
		if !ok {
			return
		}
		var ev engine.Event
		if !decode(w, r, &ev) {
			return
		}
		out, err := s.in(r).Emit(r.Context(), ev, who)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusAccepted, out)
	})
	mux.HandleFunc("GET /v1/events", s.raw(func(r *http.Request) (json.RawMessage, error) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		return s.in(r).Events(r.Context(), limit)
	}))
	mux.HandleFunc("GET /v1/triggers", s.raw(func(r *http.Request) (json.RawMessage, error) { return s.in(r).Triggers(r.Context()) }))
	mux.HandleFunc("GET /v1/registry", func(w http.ResponseWriter, r *http.Request) {
		// The server's own tools are offered to every workspace. A tool from
		// an MCP connection belongs to the workspace that holds the connection.
		tools := []engine.WorkspaceTool{}
		for _, t := range reg.Describe(reg.ToolNames()...) {
			tools = append(tools, engine.WorkspaceTool{Name: t.Name, Description: t.Description, Class: t.Class, InputSchema: t.InputSchema})
		}
		own, err := s.in(r).WorkspaceTools(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]any{"handlers": reg.HandlerNames(), "tools": append(tools, own...), "decide_providers": spec.DecideProviders})
	})
	mux.HandleFunc("POST /v1/templates/{key}", s.template)
	mux.HandleFunc("PUT /v1/connections/{name}", s.connection)
	mux.HandleFunc("POST /v1/connections/{name}/check", s.checkConnection)
	mux.HandleFunc("POST /v1/runs", s.start)
	mux.HandleFunc("GET /v1/runs/{id}", s.run)
	mux.HandleFunc("GET /v1/runs/{id}/stream", s.streamRun)
	mux.HandleFunc("GET /v1/stream", s.streamWorkspace)
	mux.HandleFunc("POST /v1/runs/{id}/resume", s.resume)
	mux.HandleFunc("POST /v1/runs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		who, ok := needActor(w, r)
		if !ok {
			return
		}
		var in struct {
			Reason string `json:"reason"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := s.in(r).Cancel(r.Context(), r.PathValue("id"), who, in.Reason); err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]string{"status": "cancelled"})
	})
	mux.HandleFunc("GET /v1/tasks", s.tasks)
	mux.HandleFunc("POST /v1/tasks/{id}/decision", s.decide)
	mux.HandleFunc("POST /v1/callbacks/{token}", s.callback)
	mux.HandleFunc("POST /v1/webhooks/{workspace}/{connection}", s.webhook)
	// An agent in a container of its own: its job, the directory it starts
	// from, and the one result it sends. The token is its credential.
	mux.HandleFunc("GET /v1/agent/{token}/job", s.raw(func(r *http.Request) (json.RawMessage, error) {
		return s.eng.AgentJob(r.Context(), r.PathValue("token"))
	}))
	mux.HandleFunc("GET /v1/agent/{token}/workspace", s.agentWorkspace)
	mux.HandleFunc("POST /v1/agent/{token}/result", s.agentResult)
	mux.HandleFunc("GET /v1/tools/{token}", s.toolList)
	mux.HandleFunc("POST /v1/tools/{token}/call", s.toolCall)
	mux.HandleFunc("GET /v1/workspaces", func(w http.ResponseWriter, r *http.Request) {
		out, err := eng.Workspaces(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		// A workspace's key is shown its own workspace and no other.
		if p := principalOf(r); p != nil && p.workspace != "" {
			mine := out[:0]
			for _, ws := range out {
				if ws.Slug == p.workspace {
					mine = append(mine, ws)
				}
			}
			out = mine
		}
		reply(w, http.StatusOK, out)
	})
	mux.HandleFunc("GET /v1/keys", func(w http.ResponseWriter, r *http.Request) {
		keys, err := s.in(r).Keys(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusOK, keys)
	})
	mux.HandleFunc("POST /v1/keys", func(w http.ResponseWriter, r *http.Request) {
		who, ok := needActor(w, r)
		if !ok {
			return
		}
		var in struct {
			Name  string `json:"name"`
			Role  string `json:"role"`
			ActAs bool   `json:"act_as"`
		}
		if !decode(w, r, &in) {
			return
		}
		secret, key, err := s.in(r).CreateKey(r.Context(), in.Name, in.Role, in.ActAs, who)
		if err != nil {
			fail(w, err)
			return
		}
		// The only time the secret is ever shown.
		reply(w, http.StatusCreated, map[string]any{"key": key, "secret": secret})
	})
	mux.HandleFunc("DELETE /v1/keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.in(r).RevokeKey(r.Context(), r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]string{"status": "revoked"})
	})
	mux.HandleFunc("POST /v1/workspaces", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := needActor(w, r); !ok {
			return
		}
		var in struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := eng.CreateWorkspace(r.Context(), in.Slug, in.Name); err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusCreated, map[string]string{"slug": in.Slug})
	})
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		// Who the server takes the caller to be, so that the console can show
		// it and need not ask for a name the server will not use.
		me := map[string]any{"name": "", "role": "", "workspace": "", "signed_in": false}
		if p := principalOf(r); p != nil {
			me = map[string]any{"name": p.name, "role": p.role, "workspace": p.workspace, "signed_in": p.signedIn}
		}
		reply(w, http.StatusOK, me)
	})
	return s.front(s.scoped(mux))
}

// WorkspaceHeader names the workspace a request is for. A request without it
// is for the default workspace.
const WorkspaceHeader = "X-Weir-Workspace"

type engineKey struct{}

// scoped resolves the request's workspace before any handler runs, so that no
// handler can read or change another workspace's data.
func (s *server) scoped(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := r.Header.Get(WorkspaceHeader)
		if slug == "" {
			// A browser's event stream cannot set headers, so a stream may
			// name its workspace in the address.
			slug = r.URL.Query().Get("workspace")
		}
		if (s.adminKey != "" || s.proxyUser != "") && !open(r.URL.Path) {
			var ok bool
			if r, slug, ok = s.authenticate(w, r, slug); !ok {
				return
			}
		}
		if slug == "" {
			slug = "default"
		}
		eng, err := s.eng.In(r.Context(), slug)
		if err != nil {
			fail(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), engineKey{}, eng)))
	})
}

// in returns the engine for the request's workspace.
func (s *server) in(r *http.Request) *engine.Engine {
	if eng, ok := r.Context().Value(engineKey{}).(*engine.Engine); ok {
		return eng
	}
	return s.eng
}

// raw serves JSON the engine has already built.
func (s *server) raw(get func(*http.Request) (json.RawMessage, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := get(r)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}
}

type server struct {
	eng *engine.Engine
	reg *worker.Registry
	// adminKey, when set, turns authentication on.
	adminKey string
	// proxyUser names the header a sign-in proxy sets to say who is asking.
	proxyUser string
	// console is the directory the console's files are served from.
	console string
	// prefix is a path the API also answers under.
	prefix string
}

// actor says who a request is made by. With authentication on, that is the
// key's own name, unless the key may speak for someone else and names them.
func actor(r *http.Request) string {
	named := r.Header.Get("X-Weir-Actor")
	if p := principalOf(r); p != nil {
		if p.actAs && named != "" {
			return named
		}
		return p.name
	}
	return named
}

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
	case errors.Is(err, worker.ErrToolFailed):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, engine.ErrStaleToken), errors.Is(err, engine.ErrTaskClosed), errors.Is(err, engine.ErrNotBlocked), errors.Is(err, engine.ErrResultSent):
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
	out, err := s.in(r).Publish(r.Context(), source, who)
	if err != nil {
		fail(w, err)
		return
	}
	published := map[string]any{"namespace": out.Namespace, "name": out.Name, "version": out.Version,
		"not_yet_enforced": out.Unsupported}
	if len(out.Warnings) > 0 {
		published["warnings"] = out.Warnings
	}
	reply(w, http.StatusCreated, published)
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
	version, err := s.in(r).PublishTemplate(r.Context(), r.PathValue("key"), t, who)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusCreated, map[string]any{"key": r.PathValue("key"), "version": version})
}

func (s *server) connection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BaseURL string `json:"base_url"`
		// Kind is "http" (the default) or "mcp". An MCP connection brings the
		// server's tools into this workspace.
		Kind string `json:"kind"`
		// ReadOnly marks every tool of an MCP connection as a read.
		ReadOnly bool `json:"read_only"`
		// Auth says how Weir signs in: a kind, and a reference to a secret.
		// The secret itself is never sent here and never stored.
		Auth engine.Auth `json:"auth"`
		// AllowMerge says that pull requests may be merged through this
		// connection. Left out, they may not.
		AllowMerge bool `json:"allow_merge"`
		// Webhook says how the third party signs what it sends to Weir. Left
		// out, whatever was set before is kept.
		Webhook *engine.Webhook `json:"webhook"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.BaseURL == "" {
		reply(w, http.StatusBadRequest, map[string]string{"error": "a connection needs a base_url"})
		return
	}
	if in.Kind == "mcp" {
		// Ask the server what it offers now, so that a workflow naming a tool
		// it does not have is refused when it is published.
		if err := in.Auth.Check(); err != nil {
			reply(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if in.Auth.Kind == secrets.GitHubApp {
			reply(w, http.StatusBadRequest, map[string]string{"error": "an MCP server cannot be signed in to as a GitHub App"})
			return
		}
		to := engine.Endpoint{Connection: r.PathValue("name"), URL: in.BaseURL, Workspace: s.in(r).Workspace(), Auth: in.Auth}
		tools, err := worker.DiscoverMCP(r.Context(), s.reg, to)
		if err != nil {
			reply(w, http.StatusBadGateway, map[string]string{"error": "the MCP server could not be asked for its tools: " + err.Error()})
			return
		}
		if err := s.in(r).PutMCPConnection(r.Context(), r.PathValue("name"), in.BaseURL, in.ReadOnly, in.Auth, tools); err != nil {
			fail(w, err)
			return
		}
		names := make([]string, len(tools))
		for i, t := range tools {
			names[i] = r.PathValue("name") + "." + t.Name
		}
		out := map[string]any{"name": r.PathValue("name"), "kind": "mcp", "tools": names}
		if !s.receives(w, r, in.Webhook, out) {
			return
		}
		reply(w, http.StatusOK, out)
		return
	}
	if in.Kind != "" && in.Kind != "http" {
		reply(w, http.StatusBadRequest, map[string]string{"error": "a connection's kind is http or mcp"})
		return
	}
	if err := s.in(r).PutConnection(r.Context(), r.PathValue("name"), in.BaseURL, in.Auth); err != nil {
		fail(w, err)
		return
	}
	if err := s.in(r).AllowMerge(r.Context(), r.PathValue("name"), in.AllowMerge); err != nil {
		fail(w, err)
		return
	}
	out := map[string]any{"name": r.PathValue("name")}
	if in.Auth.Secret != "" {
		// Say whether the secret is there yet, and where it is read from, so
		// that whoever runs the server knows what to set. Never the value.
		out["secret_found"], out["secret_read_from"] = s.reg.SecretStatus(s.in(r).Workspace(), in.Auth.Secret)
	}
	if !s.receives(w, r, in.Webhook, out) {
		return
	}
	reply(w, http.StatusOK, out)
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
	id, created, err := s.in(r).StartRun(r.Context(), engine.StartRequest{Namespace: in.Namespace, Name: in.Name, Alias: in.Alias,
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
	v, err := s.in(r).Run(r.Context(), r.PathValue("id"))
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
	if err := s.in(r).Resume(r.Context(), r.PathValue("id"), who, in.BudgetUSD); err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (s *server) tasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.in(r).Tasks(r.Context())
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
		// Data is anything the person supplies with the decision, such as
		// values they corrected. It becomes part of the step's output.
		Data json.RawMessage `json:"data"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.in(r).Decide(r.Context(), r.PathValue("id"), who, in.Outcome, in.Note, in.Data); err != nil {
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
	if err := s.in(r).Complete(r.Context(), r.PathValue("token"), engine.Result{Output: output, Actor: "callback"}); err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// toolList tells an agent which tools its current step may call. The token is
// the credential: it names one running attempt, and so one step's allow-list.
func (s *server) toolList(w http.ResponseWriter, r *http.Request) {
	runID, read, write, err := s.in(r).AllowedTools(r.Context(), r.PathValue("token"))
	if err != nil {
		fail(w, err)
		return
	}
	tools := worker.DescribeFor(r.Context(), s.eng, s.reg, runID, append(read, write...)...)
	if tools == nil {
		tools = []runner.Tool{}
	}
	reply(w, http.StatusOK, tools)
}

// toolCall runs one tool on an agent's behalf. The engine decides whether the
// step may call it, and every call is recorded, allowed or not.
func (s *server) toolCall(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := worker.CallTool(r.Context(), s.eng, s.reg, r.PathValue("token"), in.Name, in.Arguments)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"result": out})
}

// receives stores how a connection's webhooks are signed, when the request
// says, and adds to out the address to give the third party. It returns false
// after replying with an error.
func (s *server) receives(w http.ResponseWriter, r *http.Request, hook *engine.Webhook, out map[string]any) bool {
	if hook == nil {
		return true
	}
	eng, name := s.in(r), r.PathValue("name")
	if err := eng.SetWebhook(r.Context(), name, *hook); err != nil {
		fail(w, err)
		return false
	}
	if hook.Kind == engine.WebhookNone {
		return true
	}
	found, readFrom := s.reg.SecretStatus(eng.Workspace(), hook.Secret)
	out["webhook"] = map[string]any{
		"path": "/v1/webhooks/" + eng.Workspace() + "/" + name, "kind": hook.Kind,
		"secret_found": found, "secret_read_from": readFrom,
	}
	return true
}

// file sends one file a step of a run produced. It is always sent as a
// download: an agent wrote it, so a browser must never run it as a page.
func (s *server) file(w http.ResponseWriter, r *http.Request) {
	body, rec, err := s.in(r).OpenFile(r.Context(), r.PathValue("id"), r.PathValue("step"), r.PathValue("path"))
	if err != nil {
		fail(w, err)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", rec.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(rec.Size, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(rec.Path)}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Weir-File-Sha256", rec.SHA256)
	_, _ = io.Copy(w, body)
}

// checkConnection reports whether Weir can sign in to a connection, without
// doing anything on it. It is how a person setting one up sees that it worked.
// Only a connection that signs in as a GitHub App can be checked this way.
func (s *server) checkConnection(w http.ResponseWriter, r *http.Request) {
	to, err := s.in(r).Connection(r.Context(), r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	out := map[string]any{"name": to.Connection}
	if to.Auth.Secret != "" {
		out["secret_found"], out["secret_read_from"] = s.reg.SecretStatus(to.Workspace, to.Auth.Secret)
	}
	if to.Auth.Kind != secrets.GitHubApp {
		out["checked"], out["detail"] = false, "only a connection that signs in as a GitHub App can be checked; this one is used as it is"
		reply(w, http.StatusOK, out)
		return
	}
	app, err := s.reg.GitHubApp(r.Context(), nil, to)
	if err != nil {
		// The secret is missing or GitHub refused it. Either way the person
		// needs to know why, and the reason never holds a value.
		out["checked"], out["ok"], out["detail"] = true, false, err.Error()
		reply(w, http.StatusOK, out)
		return
	}
	out["checked"], out["ok"], out["github_app"] = true, true, app
	reply(w, http.StatusOK, out)
}

// agentWorkspace sends an agent the directory it starts from.
func (s *server) agentWorkspace(w http.ResponseWriter, r *http.Request) {
	body, err := s.eng.AgentArchive(r.Context(), r.PathValue("token"))
	if err != nil {
		fail(w, err)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", "application/gzip")
	_, _ = io.Copy(w, body)
}

// agentResult takes the one result an agent sends. What is in it is checked
// by the worker when it reads it back, exactly as for an agent that ran
// beside the worker.
func (s *server) agentResult(w http.ResponseWriter, r *http.Request) {
	err := s.eng.PutAgentResult(r.Context(), r.PathValue("token"), http.MaxBytesReader(w, r.Body, agent.MaxResult+1024))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reply(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "the result is too large"})
			return
		}
		fail(w, err)
		return
	}
	reply(w, http.StatusAccepted, map[string]string{"status": "received"})
}
