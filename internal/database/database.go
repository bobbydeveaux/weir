// Package database opens Weir's connections to Postgres: the pool the engine
// uses, and the single connection a migration uses. Both can sign in with a
// token made for each connection in place of a stored password.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// Token makes the password for one new connection. A nil Token means the
// address carries its own password, or needs none.
type Token func(ctx context.Context) (string, error)

// before returns the hook that runs before each connection is opened. It asks
// for a token and uses it as that connection's password. The token goes
// nowhere else: not into the address, a log or an error.
func before(token Token) func(context.Context, *pgx.ConnConfig) error {
	return func(ctx context.Context, cc *pgx.ConnConfig) error {
		t, err := token(ctx)
		if err != nil {
			return fmt.Errorf("make a sign-in token for the database: %w", err)
		}
		cc.Password = t
		return nil
	}
}

// Pool opens a pool of connections to the database at address. With a token,
// every connection the pool opens, now and later, signs in with a token made
// for it. A connection already open is not affected when its token expires.
func Pool(ctx context.Context, address string, token Token) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(address)
	if err != nil {
		return nil, err
	}
	if token != nil {
		cfg.BeforeConnect = before(token)
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}

// SQL opens the database at address through database/sql, which the
// migrations need. It signs in the same way as Pool.
func SQL(address string, token Token) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(address)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return stdlib.OpenDB(*cfg), nil
	}
	return stdlib.OpenDB(*cfg, stdlib.OptionBeforeConnect(before(token))), nil
}

// AWS returns a Token that signs in to Amazon RDS with IAM. It reads the host,
// port and user from address, and takes its credentials from AWS's default
// chain: EKS Pod Identity, an IAM role for a service account, an instance's
// role, or the environment. Making a token calls no AWS service; fetching the
// credentials behind it may.
func AWS(ctx context.Context, address, region string) (Token, error) {
	cfg, err := pgx.ParseConfig(address)
	if err != nil {
		return nil, err
	}
	loaded, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("read the AWS settings for signing in to the database: %w", err)
	}
	return RDSToken(cfg.Host, cfg.Port, cfg.User, region, loaded.Credentials), nil
}

// RDSToken returns a Token for one RDS endpoint and database user. Each token
// is signed by AWS's rules for the service rds-db and lasts 15 minutes, which
// is longer than opening a connection takes and is all it is needed for.
func RDSToken(host string, port uint16, user, region string, credentials aws.CredentialsProvider) Token {
	endpoint := net.JoinHostPort(host, strconv.Itoa(int(port)))
	return func(ctx context.Context) (string, error) {
		return auth.BuildAuthToken(ctx, endpoint, region, user, credentials)
	}
}
