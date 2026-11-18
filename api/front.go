package api

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// WithConsole serves the console's files from dir at every address that is
// not the API's, so that one server is the whole of Weir. An address that
// names no file gets index.html: the console is a single-page app and finds
// its own way from there.
func WithConsole(dir string) Option {
	return func(s *server) { s.console = dir }
}

// WithPrefix makes the API answer under prefix as well as at the root, for a
// proxy that only passes on addresses under a path of its own, such as /api.
func WithPrefix(prefix string) Option {
	return func(s *server) { s.prefix = "/" + strings.Trim(prefix, "/") }
}

// WithProxyUser trusts the named header for who is asking. A sign-in proxy
// in front of Weir sets it once a person has signed in; that person may then
// do anything, and is the actor on everything they do.
//
// This is only safe where nothing but the proxy can reach the server: anyone
// who can reach it directly can set the header themselves.
func WithProxyUser(header string) Option {
	return func(s *server) { s.proxyUser = header }
}

// signedIn returns the person a sign-in proxy says is asking, or "".
func (s *server) signedIn(r *http.Request) string {
	if s.proxyUser == "" {
		return ""
	}
	who := strings.TrimSpace(r.Header.Get(s.proxyUser))
	// Google's proxy writes the address as accounts.google.com:person@example.com.
	if i := strings.LastIndex(who, ":"); i >= 0 {
		who = who[i+1:]
	}
	return who
}

// front puts the prefix and the console in front of the API.
func (s *server) front(api http.Handler) http.Handler {
	h := api
	if s.console != "" {
		h = consoleOr(s.console, h)
	}
	if s.prefix != "" && s.prefix != "/" {
		h = unprefixed(s.prefix, h)
	}
	return h
}

func unprefixed(prefix string, next http.Handler) http.Handler {
	stripped := http.StripPrefix(prefix, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, prefix+"/") {
			stripped.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func consoleOr(dir string, api http.Handler) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		reading := r.Method == http.MethodGet || r.Method == http.MethodHead
		if !reading || p == "/healthz" || p == "/v1" || strings.HasPrefix(p, "/v1/") {
			api.ServeHTTP(w, r)
			return
		}
		clean := path.Clean("/" + p)
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(clean))); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}
