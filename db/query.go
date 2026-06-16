package db

import (
	"database/sql"
	"strings"
)

func Rebind(query string) string {
	if !strings.Contains(query, "?") {
		return query
	}
	var b strings.Builder
	n := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			b.WriteByte('$')
			b.WriteString(itoa(n))
			n++
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

func Exec(query string, args ...any) (sql.Result, error) {
	return DB.Exec(Rebind(query), args...)
}

func Query(query string, args ...any) (*sql.Rows, error) {
	return DB.Query(Rebind(query), args...)
}

func QueryRow(query string, args ...any) *sql.Row {
	return DB.QueryRow(Rebind(query), args...)
}

type Tx struct {
	tx *sql.Tx
}

func Begin() (*Tx, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx}, nil
}

func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.Exec(Rebind(query), args...)
}

func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.tx.Query(Rebind(query), args...)
}

func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRow(Rebind(query), args...)
}

func (t *Tx) Commit() error {
	return t.tx.Commit()
}

func (t *Tx) Rollback() error {
	return t.tx.Rollback()
}
