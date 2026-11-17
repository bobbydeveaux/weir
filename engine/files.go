package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"

	"github.com/jackc/pgx/v5"
)

// ErrNoFileStore is returned when files must be kept and there is nowhere to keep them.
var ErrNoFileStore = errors.New("this server has no file store")

// NewFile is a file a step produced, before it has been kept.
type NewFile struct {
	// Path is where the step wrote it, relative to the step's own directory.
	Path string
	Data []byte
}

// FileRecord says where a kept file is and what it holds.
type FileRecord struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type"`
	// Key is the file's key in the file store. It is never shown to a caller.
	Key string `json:"-"`
}

// KeepFiles puts the files a step produced into the file store, under the
// run's own workspace, and returns their records. The records mean nothing
// until they are handed to Complete: if the step does not complete, call
// DropFiles with them.
func (e *Engine) KeepFiles(ctx context.Context, runID string, files []NewFile) ([]FileRecord, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if e.Files == nil {
		return nil, ErrNoFileStore
	}
	var workspace string
	if err := e.pool.QueryRow(ctx, `SELECT workspace_id::text FROM runs WHERE id = $1::uuid`, runID).Scan(&workspace); err != nil {
		return nil, fmt.Errorf("run %s: %w", runID, ErrNotFound)
	}
	var kept []FileRecord
	for _, f := range files {
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			e.DropFiles(ctx, kept)
			return nil, err
		}
		// The key holds no part of the path the agent chose, so nothing an
		// agent writes can decide where in the store a file lands.
		rec := FileRecord{Path: f.Path, Key: workspace + "/runs/" + runID + "/" + hex.EncodeToString(id), ContentType: contentType(f.Path, f.Data)}
		stored, err := e.Files.Put(ctx, rec.Key, bytes.NewReader(f.Data), rec.ContentType)
		if err != nil {
			e.DropFiles(ctx, kept)
			return nil, fmt.Errorf("keep %s: %w", f.Path, err)
		}
		rec.Size, rec.SHA256 = stored.Size, stored.SHA256
		kept = append(kept, rec)
	}
	return kept, nil
}

// DropFiles removes files from the store that no step ended up owning. It
// does its best and reports nothing: a file left behind is wasted space, not
// a wrong answer.
func (e *Engine) DropFiles(ctx context.Context, files []FileRecord) {
	if e.Files == nil {
		return
	}
	for _, f := range files {
		_ = e.Files.Delete(context.WithoutCancel(ctx), f.Key)
	}
}

// contentType works out a file's type from its name, and failing that from
// how it begins.
func contentType(name string, data []byte) string {
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return http.DetectContentType(data)
}

// recordFiles records the files a step produced, in the transaction that
// completes it.
func (e *Engine) recordFiles(ctx context.Context, tx pgx.Tx, runID, stepID string, files []FileRecord) error {
	for _, f := range files {
		if _, err := tx.Exec(ctx, `
			INSERT INTO run_files (workspace_id, run_id, step_id, path, size, sha256, content_type, blob_key)
			SELECT r.workspace_id, r.id, $2::uuid, $3, $4, $5, $6, $7 FROM runs r WHERE r.id = $1::uuid`,
			runID, stepID, f.Path, f.Size, f.SHA256, f.ContentType, f.Key); err != nil {
			return fmt.Errorf("record file %s: %w", f.Path, err)
		}
	}
	return nil
}

// latestFiles is the files of each step's most recent visit. A step that ran
// again and wrote nothing has no files, whatever an earlier visit wrote.
const latestFiles = `
	SELECT s.step_key AS step, f.path, f.size, f.sha256, f.content_type, f.blob_key, f.created_at
	FROM run_files f JOIN steps s ON s.id = f.step_id
	WHERE f.run_id = $1::uuid AND s.compensates IS NULL
		AND s.visit = (SELECT max(x.visit) FROM steps x WHERE x.run_id = s.run_id AND x.step_key = s.step_key AND x.compensates IS NULL)`

// RunFiles lists the files a run's steps produced.
func (e *Engine) RunFiles(ctx context.Context, runID string) (json.RawMessage, error) {
	var found bool
	if err := e.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM runs WHERE id = $1::uuid AND workspace_id = $2::uuid)`,
		runID, e.workspace).Scan(&found); err != nil || !found {
		return nil, fmt.Errorf("run %s: %w", runID, ErrNotFound)
	}
	return e.list(ctx, `SELECT q.step, q.path, q.size, q.sha256, q.content_type, q.created_at FROM (`+latestFiles+`) q
		ORDER BY q.created_at, q.step, q.path`, runID)
}

// OpenFile opens one file a step of a run produced. The run must be in this
// engine's workspace.
func (e *Engine) OpenFile(ctx context.Context, runID, step, filePath string) (io.ReadCloser, FileRecord, error) {
	var rec FileRecord
	err := e.pool.QueryRow(ctx, `
		SELECT q.path, q.size, q.sha256, q.content_type, q.blob_key FROM (`+latestFiles+`) q
		JOIN runs r ON r.id = $1::uuid AND r.workspace_id = $4::uuid
		WHERE q.step = $2 AND q.path = $3`, runID, step, filePath, e.workspace).
		Scan(&rec.Path, &rec.Size, &rec.SHA256, &rec.ContentType, &rec.Key)
	if err != nil {
		return nil, rec, fmt.Errorf("file %s of step %s: %w", filePath, step, ErrNotFound)
	}
	if e.Files == nil {
		return nil, rec, ErrNoFileStore
	}
	body, err := e.Files.Get(ctx, rec.Key)
	return body, rec, err
}

// StepFiles reads every file a step of a run produced, for a later step of
// the same run. It is for the worker, which serves every workspace.
func (e *Engine) StepFiles(ctx context.Context, runID, step string) ([]NewFile, error) {
	rows, err := e.pool.Query(ctx, `SELECT q.path, q.blob_key FROM (`+latestFiles+`) q WHERE q.step = $2 ORDER BY q.path`, runID, step)
	if err != nil {
		return nil, err
	}
	type entry struct{ path, key string }
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (entry, error) {
		var en entry
		return en, row.Scan(&en.path, &en.key)
	})
	if err != nil || len(list) == 0 {
		return nil, err
	}
	if e.Files == nil {
		return nil, ErrNoFileStore
	}
	var out []NewFile
	for _, en := range list {
		body, err := e.Files.Get(ctx, en.key)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", en.path, err)
		}
		data, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", en.path, err)
		}
		out = append(out, NewFile{Path: en.path, Data: data})
	}
	return out, nil
}

// filesByStep is what expressions see of a run's files: for each step, a
// list of path, size, sha256 and content_type.
func (e *Engine) filesByStep(ctx context.Context, tx pgx.Tx, runID string) (map[string][]any, error) {
	rows, err := tx.Query(ctx, `SELECT q.step, q.path, q.size, q.sha256, q.content_type FROM (`+latestFiles+`) q ORDER BY q.step, q.path`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]any{}
	for rows.Next() {
		var step, p, sum, kind string
		var size int64
		if err := rows.Scan(&step, &p, &size, &sum, &kind); err != nil {
			return nil, err
		}
		out[step] = append(out[step], map[string]any{"path": p, "size": float64(size), "sha256": sum, "content_type": kind})
	}
	return out, rows.Err()
}
