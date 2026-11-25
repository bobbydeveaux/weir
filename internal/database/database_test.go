package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bobbydeveaux/weir/engine/store"
)

func TestAnRDSTokenNamesItsHostUserAndRegion(t *testing.T) {
	token := RDSToken("db.example.com", 5432, "weir", "eu-west-2", credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "not-a-real-secret", ""))
	got, err := token(context.Background())
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	// A token is an address with no scheme: host:port?query.
	if !strings.HasPrefix(got, "db.example.com:5432?") {
		t.Fatalf("token = %s; want it to start with the host and port", got)
	}
	q, err := url.ParseQuery(strings.SplitN(got, "?", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("Action") != "connect" || q.Get("DBUser") != "weir" {
		t.Errorf("Action = %q, DBUser = %q; want connect and weir", q.Get("Action"), q.Get("DBUser"))
	}
	if scope := q.Get("X-Amz-Credential"); !strings.HasPrefix(scope, "AKIDEXAMPLE/") || !strings.HasSuffix(scope, "/eu-west-2/rds-db/aws4_request") {
		t.Errorf("X-Amz-Credential = %q; want it signed for eu-west-2 and the service rds-db", scope)
	}
	if q.Get("X-Amz-Expires") != "900" {
		t.Errorf("X-Amz-Expires = %q; want 900 seconds", q.Get("X-Amz-Expires"))
	}
	if q.Get("X-Amz-Signature") == "" {
		t.Error("the token is not signed")
	}
	if strings.Contains(got, "not-a-real-secret") {
		t.Error("the token holds the secret key")
	}
}

// guarded is a Postgres role that can sign in only with a password, and what
// is needed to connect as it. It stands in for a role on RDS that accepts only
// an IAM token: in both, the password is something made for the connection.
type guarded struct {
	t       *testing.T
	admin   *pgxpool.Pool
	role    string
	address string // holds no password

	mu       sync.Mutex
	password string
	made     int
}

func newGuarded(ctx context.Context, t *testing.T) *guarded {
	t.Helper()
	base := os.Getenv("WEIR_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("WEIR_TEST_DATABASE_URL is not set; run `make test-db` to test against Postgres")
	}
	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	g := &guarded{t: t, admin: admin, role: "weir_iam_" + hex.EncodeToString(raw), password: "first-" + hex.EncodeToString(raw)}
	// The role owns a schema of its own, so that a migration has somewhere to go.
	for _, q := range []string{
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", g.role, g.password),
		fmt.Sprintf("CREATE SCHEMA %s AUTHORIZATION %s", g.role, g.role),
	} {
		if _, err := admin.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = admin.Exec(bg, "DROP SCHEMA "+g.role+" CASCADE")
		_, _ = admin.Exec(bg, "DROP ROLE "+g.role)
		admin.Close()
	})
	cc, err := pgx.ParseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	g.address = fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=disable search_path=%s", cc.Host, cc.Port, cc.Database, g.role, g.role)

	// The tests below prove nothing on a database that lets the role in with
	// no password, so say so loudly instead of passing.
	open, err := Pool(ctx, g.address, nil)
	if err == nil {
		err = open.Ping(ctx)
		open.Close()
	}
	if err == nil {
		t.Fatal("the test database let a role in with no password, so these tests cannot tell whether a token was used")
	}
	return g
}

// token hands out the password the role has at this moment, and counts.
func (g *guarded) token(context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.made++
	return g.password, nil
}

func (g *guarded) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.made
}

// change gives the role a new password, as a token's expiry does on RDS.
func (g *guarded) change(ctx context.Context, password string) {
	g.t.Helper()
	if _, err := g.admin.Exec(ctx, fmt.Sprintf("ALTER ROLE %s PASSWORD '%s'", g.role, password)); err != nil {
		g.t.Fatal(err)
	}
	g.mu.Lock()
	g.password = password
	g.mu.Unlock()
}

func answers(ctx context.Context, t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	var one int
	if err := conn.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("the connection does not answer: %v", err)
	}
}

func TestEachNewConnectionSignsInWithATokenOfItsOwn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	g := newGuarded(ctx, t)

	pool, err := Pool(ctx, g.address, g.token)
	if err != nil {
		t.Fatalf("Pool: %v", err)
	}
	defer pool.Close()

	// Two connections held at once must be two connections.
	first, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("the first connection: %v", err)
	}
	defer first.Release()
	second, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("the second connection: %v", err)
	}
	defer second.Release()
	answers(ctx, t, first)
	answers(ctx, t, second)
	if got := g.count(); got != 2 {
		t.Errorf("a token was made %d times for two connections; want 2", got)
	}
}

func TestAnOpenConnectionOutlivesItsToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	g := newGuarded(ctx, t)

	pool, err := Pool(ctx, g.address, g.token)
	if err != nil {
		t.Fatalf("Pool: %v", err)
	}
	defer pool.Close()
	first, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("the first connection: %v", err)
	}
	defer first.Release()

	// What the first connection signed in with is no longer accepted.
	g.change(ctx, "second-"+g.role)
	answers(ctx, t, first)

	second, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("a connection opened after the password changed: %v", err)
	}
	defer second.Release()
	answers(ctx, t, second)
	if got := g.count(); got != 2 {
		t.Errorf("a token was made %d times; want 2, one for each connection", got)
	}
}

func TestAMigrationSignsInWithAToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	g := newGuarded(ctx, t)

	db, err := SQL(g.address, g.token)
	if err != nil {
		t.Fatalf("SQL: %v", err)
	}
	defer db.Close()
	version, err := store.MigrateDB(ctx, db)
	if err != nil {
		t.Fatalf("MigrateDB: %v", err)
	}
	if version < 1 {
		t.Errorf("schema version = %d, want at least 1", version)
	}
	if g.count() < 1 {
		t.Error("the migration connected without asking for a token")
	}
	// The tables are in the role's own schema, so it was this role that made them.
	var owner string
	if err := g.admin.QueryRow(ctx, "SELECT tableowner FROM pg_tables WHERE schemaname = $1 AND tablename = 'workspaces'", g.role).Scan(&owner); err != nil || owner != g.role {
		t.Errorf("the workspaces table is owned by %q (%v); want %s", owner, err, g.role)
	}
}

func TestAConnectionIsNotOpenedWhenATokenCannotBeMade(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	g := newGuarded(ctx, t)

	pool, err := Pool(ctx, g.address, func(context.Context) (string, error) { return "", errors.New("no AWS credentials were found") })
	if err == nil {
		defer pool.Close()
		err = pool.Ping(ctx)
	}
	if err == nil || !strings.Contains(err.Error(), "make a sign-in token for the database") || !strings.Contains(err.Error(), "no AWS credentials were found") {
		t.Errorf("err = %v; want it to say a sign-in token could not be made, and why", err)
	}

	db, err := SQL(g.address, func(context.Context) (string, error) { return "", errors.New("no AWS credentials were found") })
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err == nil || !strings.Contains(err.Error(), "make a sign-in token for the database") {
		t.Errorf("through database/sql, err = %v; want it to say a sign-in token could not be made", err)
	}
}

func TestATokenTheDatabaseRefusesIsNotInTheError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	g := newGuarded(ctx, t)

	const wrong = "a-token-that-must-stay-out-of-errors"
	pool, err := Pool(ctx, g.address, func(context.Context) (string, error) { return wrong, nil })
	if err == nil {
		defer pool.Close()
		err = pool.Ping(ctx)
	}
	if err == nil {
		t.Fatal("the database accepted a wrong token")
	}
	if strings.Contains(err.Error(), wrong) {
		t.Errorf("the error holds the token: %v", err)
	}
}
