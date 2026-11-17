// Package secrets turns a reference to a secret into its value, at the moment
// a call is made. Weir never stores a secret: a connection holds a reference,
// and the platform Weir runs on supplies the value, as an environment variable
// or a file. Every platform can do that: Cloud Run and Secret Manager, AWS and
// Secrets Manager, Azure and Key Vault, Kubernetes and a Secret.
//
// A workspace can name only its own secrets. A reference is a short name, not
// a variable or a path, so a workspace cannot reach the server's own settings,
// its files, or another workspace's secrets:
//
//	env:GITHUB_TOKEN    in workspace acme  ->  $WEIR_SECRET__ACME__GITHUB_TOKEN
//	file:github-token   in workspace acme  ->  $WEIR_SECRETS_DIR/acme/github-token
package secrets

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The ways a connection can prove who Weir is.
const (
	None   = "none"
	Bearer = "bearer" // Authorization: Bearer <secret>
	Header = "header" // <header>: <secret>
	Basic  = "basic"  // Authorization: Basic base64(<username>:<secret>)
	// GitHubApp signs in as a GitHub App. The secret is the App's private
	// key, and is never sent anywhere: it signs a request for a short-lived
	// token for one repository. Only the GitHub tools can use it.
	GitHubApp = "github-app"
)

// Auth says how to sign in to a connection. It holds a reference to the
// secret, never the secret.
type Auth struct {
	// Kind is none, bearer, header or basic. Empty means none.
	Kind string `json:"kind"`
	// Secret is the reference: env:NAME or file:name.
	Secret string `json:"secret,omitempty"`
	// Header is the header to send, for kind header.
	Header string `json:"header,omitempty"`
	// Username is the user name, for kind basic.
	Username string `json:"username,omitempty"`
	// App is the GitHub App's client id or App id, for kind github-app.
	App string `json:"app,omitempty"`
}

var (
	envName    = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)
	fileName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	appName    = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
	headerName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
)

// Check reports whether a is well formed. It does not look for the secret.
func (a Auth) Check() error {
	switch a.Kind {
	case "", None:
		if a.Secret != "" || a.Header != "" || a.Username != "" || a.App != "" {
			return errors.New("auth has a secret, a header, a user name or an app but no kind: set kind to bearer, header, basic or github-app")
		}
		return nil
	case Bearer:
	case Header:
		if !headerName.MatchString(a.Header) {
			return errors.New(`auth of kind header needs the header's name, such as "X-Api-Key"`)
		}
		// Weir sets these itself; a connection may not take them over.
		switch strings.ToLower(a.Header) {
		case "host", "content-type", "content-length", "idempotency-key":
			return fmt.Errorf("auth may not use the %s header", a.Header)
		}
	case Basic:
		if a.Username == "" || strings.Contains(a.Username, ":") {
			return errors.New("auth of kind basic needs a user name, with no colon in it")
		}
	case GitHubApp:
		if !appName.MatchString(a.App) {
			return errors.New("auth of kind github-app needs app: the App's client id or App id, from its settings page on GitHub")
		}
	default:
		return fmt.Errorf("auth kind %q is not one of none, bearer, header, basic, github-app", a.Kind)
	}
	return CheckRef(a.Secret)
}

// CheckRef reports whether ref is a well-formed reference to a secret.
func CheckRef(ref string) error {
	scheme, name, ok := strings.Cut(ref, ":")
	switch {
	case !ok || name == "":
		return fmt.Errorf("a secret is named as env:NAME or file:name, not %q", ref)
	case scheme == "env" && envName.MatchString(name):
		return nil
	case scheme == "env":
		return fmt.Errorf("env:%s: the name must be capitals, digits and single underscores, such as GITHUB_TOKEN", name)
	case scheme == "file" && fileName.MatchString(name) && !strings.Contains(name, ".."):
		return nil
	case scheme == "file":
		return fmt.Errorf("file:%s: the name must be a plain file name with no directory, such as github-token", name)
	}
	return fmt.Errorf("a secret is named as env:NAME or file:name, not %q", ref)
}

// Resolver finds the value of a secret.
type Resolver struct {
	// Getenv reads an environment variable. Nil means os.Getenv.
	Getenv func(string) string
	// Dir is the directory that holds one directory of secret files per
	// workspace. Empty means file: references cannot be used.
	Dir string
}

// FromEnv returns a Resolver that reads this process's environment, with the
// directory for files taken from WEIR_SECRETS_DIR.
func FromEnv(getenv func(string) string) *Resolver {
	if getenv == nil {
		getenv = os.Getenv
	}
	return &Resolver{Getenv: getenv, Dir: getenv("WEIR_SECRETS_DIR")}
}

// Where says where the value of ref is looked for, for a workspace: the name
// of the variable, or the path of the file. It is safe to show to a person.
func (r *Resolver) Where(workspace, ref string) (string, error) {
	if err := CheckRef(ref); err != nil {
		return "", err
	}
	if workspace == "" || strings.Contains(workspace, "--") || strings.ContainsAny(workspace, "/\\._") {
		return "", fmt.Errorf("workspace %q cannot hold secrets: its name must not contain a double hyphen", workspace)
	}
	scheme, name, _ := strings.Cut(ref, ":")
	if scheme == "env" {
		// Double underscores mark the two joins. A workspace name has no
		// underscore and a secret name has no double one, so no two pairs of
		// workspace and name can make the same variable.
		return "WEIR_SECRET__" + strings.ToUpper(strings.ReplaceAll(workspace, "-", "_")) + "__" + name, nil
	}
	if r.Dir == "" {
		return "", errors.New("file: secrets need WEIR_SECRETS_DIR to be set on the server")
	}
	return filepath.Join(r.Dir, workspace, name), nil
}

// Resolve returns the value of ref for a workspace. The error names the
// variable or file to set, and never contains a value.
func (r *Resolver) Resolve(workspace, ref string) (string, error) {
	where, err := r.Where(workspace, ref)
	if err != nil {
		return "", err
	}
	var value string
	if strings.HasPrefix(ref, "env:") {
		getenv := r.Getenv
		if getenv == nil {
			getenv = os.Getenv
		}
		value = getenv(where)
	} else {
		raw, err := os.ReadFile(where)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("the secret %s could not be read from %s", ref, where)
		}
		value = strings.TrimRight(string(raw), "\r\n")
	}
	if value == "" {
		return "", fmt.Errorf("the secret %s is not set on this server: it is read from %s", ref, where)
	}
	return value, nil
}

// Apply signs a request for a connection in a workspace. A connection with no
// auth is left as it is. If the secret cannot be found the request must not be
// sent: Apply returns an error and adds nothing.
func (r *Resolver) Apply(req *http.Request, workspace string, a Auth) error {
	if a.Kind == "" || a.Kind == None {
		return nil
	}
	if err := a.Check(); err != nil {
		return err
	}
	if a.Kind == GitHubApp {
		// The App's key signs requests for tokens; it is not itself sent.
		return errors.New("it signs in as a GitHub App, which only the GitHub tools can use")
	}
	value, err := r.Resolve(workspace, a.Secret)
	if err != nil {
		return err
	}
	switch a.Kind {
	case Bearer:
		req.Header.Set("Authorization", "Bearer "+value)
	case Header:
		req.Header.Set(a.Header, value)
	case Basic:
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(a.Username+":"+value)))
	}
	return nil
}

// SameHost is a redirect rule for an http.Client that carries credentials: a
// redirect to another host is not followed, so that a third party cannot send
// a connection's credential somewhere else. The caller gets the redirect
// itself as the reply.
func SameHost(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if len(via) > 0 && req.URL.Host != via[0].URL.Host {
		return http.ErrUseLastResponse
	}
	return nil
}
