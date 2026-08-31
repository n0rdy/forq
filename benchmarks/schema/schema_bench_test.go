// Package schema benchmarks the messages table schema variants: the current
// WITHOUT ROWID table vs a plain rowid table.
//
// Why: SQLite's own guidance says WITHOUT ROWID tables work best when rows are
// smaller than ~1/20th of a page (~200 bytes at the default 4KB page size),
// while Forq payloads are typically 1-50KB - every row spills into overflow
// pages. This benchmark measures whether that actually hurts the hot path
// (insert -> claim -> ack) using Forq's exact DDL, pragmas, and queries.
//
// Run:
//
//	go test -bench=. -benchtime=2s ./schema/
package schema

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	_ "github.com/mattn/go-sqlite3"
)

const (
	readyStatus      = 0
	processingStatus = 1
)

var payloadSizes = []struct {
	name string
	size int
}{
	{"1KB", 1 * 1024},
	{"10KB", 10 * 1024},
	{"50KB", 50 * 1024},
}

var schemaVariants = []struct {
	name         string
	withoutRowid bool
}{
	{"without_rowid", true},
	{"rowid", false},
}

// setupDB creates a fresh DB with Forq's exact schema (modulo the WITHOUT
// ROWID clause), indexes, and write-connection pragmas.
func setupDB(b *testing.B, withoutRowid bool) *sql.DB {
	b.Helper()

	dbPath := filepath.Join(b.TempDir(), "bench.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		b.Fatal(err)
	}
	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA cache_size = -40000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			b.Fatal(err)
		}
	}

	rowidClause := ""
	if withoutRowid {
		rowidClause = " WITHOUT ROWID"
	}

	ddl := []string{
		`CREATE TABLE messages
		(
			id                    TEXT PRIMARY KEY,
			queue                 TEXT    NOT NULL,
			is_dlq                BOOLEAN NOT NULL DEFAULT FALSE,
			content               TEXT    NOT NULL,
			status                INTEGER NOT NULL DEFAULT 0,
			attempts              INTEGER NOT NULL DEFAULT 0,
			process_after         INTEGER NOT NULL,
			processing_started_at INTEGER,
			failure_reason        TEXT,
			received_at           INTEGER NOT NULL,
			updated_at            INTEGER NOT NULL,
			expires_after         INTEGER NOT NULL
		)` + rowidClause + `;`,
		`CREATE INDEX idx_queue_ready_for_consuming ON messages (queue, status, received_at, process_after) WHERE status = 0;`,
		`CREATE INDEX idx_for_queue_depth ON messages (queue, is_dlq);`,
		`CREATE INDEX idx_expired ON messages (status, is_dlq, expires_after);`,
		`CREATE INDEX idx_for_requeueuing ON messages (queue, status);`,
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			b.Fatal(err)
		}
	}

	b.Cleanup(func() { db.Close() })
	return db
}

func insertMessage(db *sql.DB, queue, content string) (string, error) {
	id := uuid.NewV7()
	nowMs := time.Now().UnixMilli()
	_, err := db.Exec(
		`INSERT INTO messages (id, queue, content, process_after, received_at, updated_at, expires_after)
		 VALUES (?, ?, ?, ?, ?, ?, ?);`,
		id.String(), queue, content, nowMs, nowMs, nowMs, nowMs+24*60*60*1000,
	)
	return id.String(), err
}

// claimMessage is Forq's exact consume query.
func claimMessage(db *sql.DB, queue string) (id string, receipt int64, err error) {
	nowMs := time.Now().UnixMilli()
	var content string
	err = db.QueryRow(
		`UPDATE messages
		 SET status = ?, attempts = attempts + 1, processing_started_at = ?, updated_at = ?
		 WHERE id = (
			SELECT id FROM messages
			WHERE queue = ? AND status = ? AND process_after <= ?
			ORDER BY received_at ASC
			LIMIT 1
		 )
		 RETURNING id, content, processing_started_at;`,
		processingStatus, nowMs, nowMs, queue, readyStatus, nowMs,
	).Scan(&id, &content, &receipt)
	return id, receipt, err
}

func ackMessage(db *sql.DB, id, queue string, receipt int64) error {
	_, err := db.Exec(
		`DELETE FROM messages WHERE id = ? AND queue = ? AND status = ? AND processing_started_at = ?;`,
		id, queue, processingStatus, receipt,
	)
	return err
}

func seedBacklog(b *testing.B, db *sql.DB, queue, content string, n int) {
	b.Helper()
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	nowMs := time.Now().UnixMilli()
	stmt, err := tx.Prepare(
		`INSERT INTO messages (id, queue, content, process_after, received_at, updated_at, expires_after)
		 VALUES (?, ?, ?, ?, ?, ?, ?);`)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := uuid.NewV7()
		if _, err := stmt.Exec(id.String(), queue, content, nowMs, nowMs+int64(i), nowMs, nowMs+24*60*60*1000); err != nil {
			b.Fatal(err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
}

// BenchmarkInsert measures pure produce throughput.
func BenchmarkInsert(b *testing.B) {
	for _, variant := range schemaVariants {
		for _, payload := range payloadSizes {
			b.Run(fmt.Sprintf("%s/%s", variant.name, payload.name), func(b *testing.B) {
				db := setupDB(b, variant.withoutRowid)
				content := strings.Repeat("x", payload.size)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := insertMessage(db, "bench", content); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkProduceConsumeAck measures the full hot-path cycle against a
// steady 10k-message backlog, so the B-trees have realistic depth.
func BenchmarkProduceConsumeAck(b *testing.B) {
	for _, variant := range schemaVariants {
		for _, payload := range payloadSizes {
			b.Run(fmt.Sprintf("%s/%s", variant.name, payload.name), func(b *testing.B) {
				db := setupDB(b, variant.withoutRowid)
				content := strings.Repeat("x", payload.size)
				seedBacklog(b, db, "bench", content, 10_000)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := insertMessage(db, "bench", content); err != nil {
						b.Fatal(err)
					}
					id, receipt, err := claimMessage(db, "bench")
					if err != nil {
						b.Fatal(err)
					}
					if err := ackMessage(db, id, "bench", receipt); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
