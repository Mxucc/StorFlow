// Package store is the persistence layer of the cloud drive shop.
//
// It talks to the host application's database through the plain database/sql
// API so that the module stays independent from the ent schema. Table and
// column names are prefixed with crshop_ / belong to crshop_* tables only, the
// module never issues DDL against tables owned by StorFlow itself.
package store

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Mxucc/StorFlow/pkg/logging"
)

// Dialect identifies the SQL dialect of the underlying database.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectMySQL    Dialect = "mysql"
	DialectPostgres Dialect = "postgres"
)

// ErrNotFound is returned when a single row lookup finds nothing.
var ErrNotFound = errors.New("shop: record not found")

// ErrConflict is returned when a unique constraint would be violated.
var ErrConflict = errors.New("shop: record conflict")

// Querier is the subset of *sql.DB / *sql.Tx used by the store.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (stdsql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*stdsql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *stdsql.Row
}

// Store is the module's data access object.
type Store struct {
	db      *stdsql.DB
	dialect Dialect
	l       logging.Logger
}

// New creates a store over an existing database handle.
func New(db *stdsql.DB, dialect Dialect, l logging.Logger) *Store {
	if l == nil {
		l = logging.NewConsoleLogger(logging.LevelWarning)
	}
	return &Store{db: db, dialect: dialect, l: l}
}

// Dialect returns the SQL dialect of the underlying database.
func (s *Store) Dialect() Dialect { return s.dialect }

// conn is a dialect aware handle that can represent either the DB or one
// transaction.
type conn struct {
	q       Querier
	dialect Dialect
}

func (s *Store) conn() *conn { return &conn{q: s.db, dialect: s.dialect} }

// rebind converts the portable '?' placeholder style into whatever the
// underlying dialect expects.
func (c *conn) rebind(query string) string {
	if c.dialect != DialectPostgres {
		return query
	}
	var sb strings.Builder
	sb.Grow(len(query) + 8)
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] != '?' {
			sb.WriteByte(query[i])
			continue
		}
		n++
		sb.WriteByte('$')
		sb.WriteString(fmt.Sprintf("%d", n))
	}
	return sb.String()
}

func (c *conn) exec(ctx context.Context, query string, args ...any) (stdsql.Result, error) {
	return c.q.ExecContext(ctx, c.rebind(query), args...)
}

func (c *conn) query(ctx context.Context, query string, args ...any) (*stdsql.Rows, error) {
	return c.q.QueryContext(ctx, c.rebind(query), args...)
}

func (c *conn) queryRow(ctx context.Context, query string, args ...any) *stdsql.Row {
	return c.q.QueryRowContext(ctx, c.rebind(query), args...)
}

// Tx is a transactional view of the store handed to Store.Tx callbacks.
type Tx struct{ *conn }

// Tx runs fn inside a database transaction. The transaction is committed when
// fn returns nil and rolled back otherwise.
func (s *Store) Tx(ctx context.Context, fn func(tx *Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("shop: begin transaction: %w", err)
	}

	if err := fn(&Tx{&conn{q: tx, dialect: s.dialect}}); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, stdsql.ErrTxDone) {
			return fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("shop: commit transaction: %w", err)
	}
	return nil
}

// isUniqueViolation reports whether err is a duplicate key error of any
// supported dialect.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"unique constraint",
		"duplicate entry",
		"duplicate key value",
		"unique_violation",
		"constraint failed: unique",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, stdsql.ErrNoRows) {
		return ErrNotFound
	}
	if isUniqueViolation(err) {
		return fmt.Errorf("%s: %w", op, ErrConflict)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// boolToInt converts a boolean into the integer representation used by the
// portable DDL (sqlite has no dedicated boolean type).
func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func intToBool(v int) bool { return v != 0 }

// IsConflict reports whether err is a unique constraint violation.
func IsConflict(err error) bool { return errors.Is(err, ErrConflict) }

// IsNotFound reports whether err is a missing record error.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
