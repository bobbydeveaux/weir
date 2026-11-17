package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/bobbydeveaux/weir/engine"
)

// Option changes how the API behaves.
type Option func(*server)

// WithAdminKey turns authentication on. Every request must then carry a key:
// this one, which may do anything in any workspace, or one made for a
// workspace. Without it the API trusts its callers, which is only safe where
// every caller is trusted.
func WithAdminKey(key string) Option {
	return func(s *server) { s.adminKey = key }
}

// principal is who a request was made by, once its key has been checked.
type principal struct {
	name string
	// workspace is the only workspace the key may use. Empty means any: the admin key.
	workspace string
	role      string
	actAs     bool
}

type principalKey struct{}

func principalOf(r *http.Request) *principal {
	p, _ := r.Context().Value(principalKey{}).(*principal)
	return p
}

// open reports whether a path needs no key. A callback and a tool call carry
// a one-time token for a single attempt, which is their credential. A webhook
// carries a signature made with the connection's shared secret.
func open(path string) bool {
	return path == "/healthz" || strings.HasPrefix(path, "/v1/callbacks/") || strings.HasPrefix(path, "/v1/tools/") || strings.HasPrefix(path, "/v1/agent/") ||
		strings.HasPrefix(path, "/v1/webhooks/")
}

func unauthorized(w http.ResponseWriter, why string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="weir"`)
	reply(w, http.StatusUnauthorized, map[string]string{"error": why})
}

// authenticate checks the request's key and decides what it may do. It
// returns the workspace the request is for, or false after replying.
func (s *server) authenticate(w http.ResponseWriter, r *http.Request, asked string) (*http.Request, string, bool) {
	secret, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || secret == "" {
		unauthorized(w, "send your API key as: Authorization: Bearer <key>")
		return nil, "", false
	}
	var p *principal
	if subtle.ConstantTimeCompare([]byte(secret), []byte(s.adminKey)) == 1 {
		p = &principal{name: "admin", role: engine.RoleAdmin, actAs: true}
	} else {
		key, err := s.eng.Authenticate(r.Context(), secret)
		if errors.Is(err, engine.ErrBadKey) {
			unauthorized(w, "the API key is not valid, or has been revoked")
			return nil, "", false
		}
		if err != nil {
			fail(w, err)
			return nil, "", false
		}
		p = &principal{name: key.Name, workspace: key.Workspace, role: key.Role, actAs: key.ActAs}
	}

	// A workspace's key works in that workspace and no other.
	workspace := asked
	if p.workspace != "" {
		if asked != "" && asked != p.workspace {
			reply(w, http.StatusForbidden, map[string]string{"error": "this key belongs to the " + p.workspace + " workspace"})
			return nil, "", false
		}
		workspace = p.workspace
	}

	if why := refuse(p, r); why != "" {
		reply(w, http.StatusForbidden, map[string]string{"error": why})
		return nil, "", false
	}
	return r.WithContext(context.WithValue(r.Context(), principalKey{}, p)), workspace, true
}

// refuse says why a key may not make a request, or "" if it may.
func refuse(p *principal, r *http.Request) string {
	reading := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch {
	case r.URL.Path == "/v1/workspaces" && !reading && p.workspace != "":
		return "only the admin key can create a workspace"
	case strings.HasPrefix(r.URL.Path, "/v1/keys") && p.role != engine.RoleAdmin:
		return "managing keys needs a key with the admin role"
	case !reading && p.role == engine.RoleReader:
		return "this key can only read"
	}
	return ""
}
