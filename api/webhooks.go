package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/bobbydeveaux/weir/engine"
)

// maxWebhook is the largest webhook body read. GitHub's largest is 25 MB;
// this is well under it, and far more than an ordinary event.
const maxWebhook = 5 << 20

// webhook receives a request from a third party and turns it into an event.
//
// The sender cannot send Weir's headers or hold an API key, so the workspace
// and the connection are in the address, and the signature is the credential:
// only someone holding the connection's shared secret can produce it.
//
// What a caller is told is deliberately little. An address that does not
// exist, a connection that receives nothing, and one whose secret is not set
// all look much the same from outside.
func (s *server) webhook(w http.ResponseWriter, r *http.Request) {
	workspace, connection := r.PathValue("workspace"), r.PathValue("connection")
	notHere := func() {
		reply(w, http.StatusNotFound, map[string]string{"error": "nothing receives webhooks at this address"})
	}

	eng, err := s.eng.In(r.Context(), workspace)
	if err != nil {
		notHere()
		return
	}
	hook, err := eng.Webhook(r.Context(), connection)
	if err != nil {
		notHere()
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhook))
	if err != nil {
		reply(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "the request body is too large"})
		return
	}

	// The signature is checked before anything in the body is trusted.
	secret, err := s.reg.ResolveSecret(workspace, hook.Secret)
	if err != nil {
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "this webhook is not ready to receive"})
		return
	}
	header, prefix, eventHeader, idHeader := hook.Header, hook.Prefix, hook.EventHeader, hook.IDHeader
	if hook.Kind == engine.WebhookGitHub {
		header, prefix, eventHeader, idHeader = "X-Hub-Signature-256", "sha256=", "X-GitHub-Event", "X-GitHub-Delivery"
	}
	if !signed(r.Header.Get(header), prefix, secret, body) {
		reply(w, http.StatusUnauthorized, map[string]string{"error": "the signature does not match"})
		return
	}

	// From here the request is known to come from the holder of the secret.
	name := hook.Event
	if eventHeader != "" {
		name = strings.ToLower(r.Header.Get(eventHeader))
	}
	if !engine.EventName(name) {
		reply(w, http.StatusBadRequest, map[string]string{"error": "the request does not say what happened"})
		return
	}
	if !json.Valid(body) {
		reply(w, http.StatusBadRequest, map[string]string{"error": "the body must be JSON; set the sender's content type to application/json"})
		return
	}
	id := ""
	if idHeader != "" {
		id = r.Header.Get(idHeader)
	}
	out, err := eng.Emit(r.Context(), engine.Event{Type: connection + "." + name, ID: id, Source: connection, Data: body},
		"webhook:"+connection)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusAccepted, map[string]any{"received": true, "duplicate": out.Duplicate,
		"started": len(out.Runs), "resumed": len(out.Resumed)})
}

// signed reports whether header holds the HMAC-SHA256 of body under secret,
// as hex after prefix. The comparison takes the same time whatever differs.
func signed(header, prefix, secret string, body []byte) bool {
	given, ok := strings.CutPrefix(header, prefix)
	if !ok || given == "" {
		return false
	}
	want, err := hex.DecodeString(given)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(want, mac.Sum(nil))
}
