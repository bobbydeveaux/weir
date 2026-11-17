package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/bobbydeveaux/weir/internal/secrets"
)

// The ways a third party can sign what it sends.
const (
	WebhookNone   = "none"
	WebhookGitHub = "github" // X-Hub-Signature-256, as GitHub sends it
	WebhookHMAC   = "hmac"   // HMAC-SHA256 of the body, in a header you name
)

// Webhook says how a third party signs the requests it sends to Weir through
// a connection. It holds a reference to the shared secret, never the secret.
type Webhook struct {
	// Kind is github or hmac. "none" switches receiving off.
	Kind string `json:"kind"`
	// Secret is the reference to the shared secret: env:NAME or file:name.
	Secret string `json:"secret,omitempty"`

	// The rest apply to kind hmac. GitHub's are fixed.

	// Header carries the signature, as hex.
	Header string `json:"header,omitempty"`
	// Prefix comes before the hex in that header, such as "sha256=".
	Prefix string `json:"prefix,omitempty"`
	// EventHeader carries the name of what happened. If the sender has none,
	// set Event to a fixed name.
	EventHeader string `json:"event_header,omitempty"`
	Event       string `json:"event,omitempty"`
	// IDHeader carries the sender's id for the delivery, so that a repeat
	// starts nothing new. Without it, a repeat cannot be recognised.
	IDHeader string `json:"id_header,omitempty"`
}

var (
	webhookHeader = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
	webhookEvent  = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
)

// EventName reports whether name can be the last part of an event's type.
func EventName(name string) bool { return webhookEvent.MatchString(name) }

// Check reports whether w is well formed. It does not look for the secret.
func (w Webhook) Check() error {
	switch w.Kind {
	case WebhookNone:
		return nil
	case WebhookGitHub:
		if w.Header != "" || w.Prefix != "" || w.EventHeader != "" || w.Event != "" || w.IDHeader != "" {
			return errors.New("a github webhook takes only a secret: GitHub's headers are fixed")
		}
	case WebhookHMAC:
		if !webhookHeader.MatchString(w.Header) {
			return errors.New(`an hmac webhook needs the name of the header that carries the signature, such as "X-Signature"`)
		}
		if (w.EventHeader == "") == (w.Event == "") {
			return errors.New("an hmac webhook needs either event_header, the header that says what happened, or event, a fixed name")
		}
		if w.EventHeader != "" && !webhookHeader.MatchString(w.EventHeader) {
			return fmt.Errorf("event_header %q is not a header name", w.EventHeader)
		}
		if w.Event != "" && !EventName(w.Event) {
			return fmt.Errorf("event %q must be lower-case words joined by dots, hyphens or underscores", w.Event)
		}
		if w.IDHeader != "" && !webhookHeader.MatchString(w.IDHeader) {
			return fmt.Errorf("id_header %q is not a header name", w.IDHeader)
		}
	default:
		return fmt.Errorf("webhook kind %q is not one of github, hmac, none", w.Kind)
	}
	return secrets.CheckRef(w.Secret)
}

// SetWebhook says how a third party signs what it sends through an existing
// connection. Kind "none" stops the connection receiving anything.
func (e *Engine) SetWebhook(ctx context.Context, name string, w Webhook) error {
	if err := w.Check(); err != nil {
		return fmt.Errorf("connection %s: %w", name, err)
	}
	var stored *string
	if w.Kind != WebhookNone {
		raw, err := json.Marshal(w)
		if err != nil {
			return err
		}
		text := string(raw)
		stored = &text
	}
	tag, err := e.pool.Exec(ctx, `UPDATE connections SET webhook = $3::jsonb WHERE workspace_id = $1::uuid AND name = $2`,
		e.workspace, name, stored)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("connection %q: %w", name, ErrNotFound)
	}
	return nil
}

// Webhook returns how requests arriving through a connection are signed. It
// returns ErrNotFound if there is no such connection in this workspace, or it
// does not receive anything.
func (e *Engine) Webhook(ctx context.Context, name string) (*Webhook, error) {
	var raw *string
	err := e.pool.QueryRow(ctx, `SELECT webhook::text FROM connections WHERE workspace_id = $1::uuid AND name = $2`,
		e.workspace, name).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && raw == nil) {
		return nil, fmt.Errorf("connection %q receives no webhooks: %w", name, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	var w Webhook
	return &w, json.Unmarshal([]byte(*raw), &w)
}
