package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// InTx runs fn inside a transaction, rolling back on error or panic.
//
// Several invariants are transactional (O1–O4): a base row and its variant row
// must appear together, and — from Stage 5 — a ledger-derived write and its
// event must land together or not at all. This is the one place that guarantee
// is implemented.
func InTx(ctx context.Context, conn *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Timestamps
// ---------------------------------------------------------------------------

// TimeLayout is what the application writes. SQLite has no date type, so
// timestamps are text, and the application always writes RFC3339 in UTC.
const TimeLayout = time.RFC3339Nano

// sqliteLayouts are the formats a column may hold. The application writes
// RFC3339, but a DEFAULT (datetime('now')) produces SQLite's own format, so
// reads must accept both.
var sqliteLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// FormatTime renders a timestamp for storage.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// ParseTime reads a stored timestamp.
func ParseTime(s string) (time.Time, error) {
	for _, layout := range sqliteLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp %q", s)
}

// FormatNullTime renders an optional timestamp for storage.
func FormatNullTime(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: FormatTime(*t), Valid: true}
}

// DateLayout is how a calendar date is stored, as distinct from an instant.
const DateLayout = "2006-01-02"

// FormatNullDate renders an optional DATE for storage.
//
// Separate from FormatNullTime on purpose. An expiry is what is printed on the
// packet, and a time of day on it is not merely useless -- it is harmful, since
// expires_on is part of H8's key and two spellings of one date would read as
// two Holdings. Storing the date makes the comparison the operations already do
// (by date) and the comparison SQL does (by string) the same comparison.
func FormatNullDate(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(DateLayout), Valid: true}
}

// ParseNullTime reads an optional stored timestamp.
func ParseNullTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := ParseTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ---------------------------------------------------------------------------
// Optional scalars
// ---------------------------------------------------------------------------

// NullInt64 wraps an optional identifier for storage.
func NullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

// PtrInt64 unwraps an optional identifier read from storage.
func PtrInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

// NullString wraps an optional string for storage. An empty string is stored as
// NULL: the distinction between "absent" and "present but empty" carries no
// meaning for any text column in this schema.
func NullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// StringOrEmpty unwraps an optional string read from storage.
func StringOrEmpty(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}
