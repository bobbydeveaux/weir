package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The roles a key can hold. Each includes the ones before it.
const (
	RoleReader = "reader" // may read
	RoleWriter = "writer" // may also publish, start, decide, resume and cancel
	RoleAdmin  = "admin"  // may also manage the workspace's keys
)

// ErrBadKey means a key is unknown or has been revoked.
var ErrBadKey = errors.New("the API key is not valid")

// Key is an API key, without its secret.
type Key struct {
	ID string `json:"id"`
	// Name is who the key speaks as: it is the actor on everything the key does.
	Name      string `json:"name"`
	Workspace string `json:"workspace"`
	Role      string `json:"role"`
	// ActAs lets the key name the person it is acting for. An application
	// that has its own sign-in holds a key like this, and passes on who its
	// user is. A key without it always acts as itself.
	ActAs      bool       `json:"act_as"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// keyPrefix marks a string as a Weir key, so that one found in a log or a
// repository can be recognised for what it is.
const keyPrefix = "weir_"

func hashKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// CreateKey makes a key for this workspace and returns its secret. The secret
// is not stored and cannot be shown again.
func (e *Engine) CreateKey(ctx context.Context, name, role string, actAs bool, by string) (secret string, key *Key, err error) {
	switch role {
	case RoleReader, RoleWriter, RoleAdmin:
	default:
		return "", nil, fmt.Errorf("a key's role is reader, writer or admin, not %q", role)
	}
	if name == "" {
		return "", nil, errors.New("a key needs a name")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	secret = keyPrefix + hex.EncodeToString(raw)
	key = &Key{Name: name, Role: role, ActAs: actAs, CreatedBy: by}
	err = e.pool.QueryRow(ctx, `
		INSERT INTO api_keys (workspace_id, name, role, act_as, key_hash, created_by)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)
		RETURNING id::text, created_at, (SELECT slug FROM workspaces WHERE id = $1::uuid)`,
		e.workspace, name, role, actAs, hashKey(secret), by).Scan(&key.ID, &key.CreatedAt, &key.Workspace)
	if err != nil {
		return "", nil, fmt.Errorf("create key %s (is there already a key with that name?): %w", name, err)
	}
	return secret, key, nil
}

// Authenticate finds the key a secret belongs to, in whichever workspace it
// is. It returns ErrBadKey for a secret that is unknown or revoked.
func (e *Engine) Authenticate(ctx context.Context, secret string) (*Key, error) {
	var k Key
	err := e.pool.QueryRow(ctx, `
		UPDATE api_keys k SET last_used_at = now()
		FROM workspaces w
		WHERE w.id = k.workspace_id AND k.key_hash = $1 AND k.revoked_at IS NULL
		RETURNING k.id::text, k.name, w.slug, k.role, k.act_as, k.created_by, k.created_at`,
		hashKey(secret)).Scan(&k.ID, &k.Name, &k.Workspace, &k.Role, &k.ActAs, &k.CreatedBy, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadKey
	}
	return &k, err
}

// Keys lists this workspace's keys, newest first. Secrets are never returned.
func (e *Engine) Keys(ctx context.Context) ([]Key, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT k.id::text, k.name, w.slug, k.role, k.act_as, k.created_by, k.created_at, k.last_used_at, k.revoked_at
		FROM api_keys k JOIN workspaces w ON w.id = k.workspace_id
		WHERE k.workspace_id = $1::uuid ORDER BY k.created_at DESC`, e.workspace)
	if err != nil {
		return nil, err
	}
	keys, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Key, error) {
		var k Key
		err := row.Scan(&k.ID, &k.Name, &k.Workspace, &k.Role, &k.ActAs, &k.CreatedBy, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
		return k, err
	})
	if keys == nil {
		keys = []Key{}
	}
	return keys, err
}

// RevokeKey stops one of this workspace's keys from working, at once.
func (e *Engine) RevokeKey(ctx context.Context, id string) error {
	tag, err := e.pool.Exec(ctx, `
		UPDATE api_keys SET revoked_at = now()
		WHERE id = $1::uuid AND workspace_id = $2::uuid AND revoked_at IS NULL`, id, e.workspace)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("key %s: %w", id, ErrNotFound)
	}
	return nil
}
