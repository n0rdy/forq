package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/db"
	"github.com/n0rdy/forq/internal/testutil"

	"github.com/google/uuid"
)

func newMessage(t *testing.T, queue string, content string) *db.NewMessage {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	nowMs := time.Now().UnixMilli()
	return &db.NewMessage{
		Id:           id.String(),
		QueueName:    queue,
		Content:      content,
		ProcessAfter: nowMs,
		ReceivedAt:   nowMs,
		UpdatedAt:    nowMs,
		ExpiresAfter: nowMs + 24*60*60*1000,
	}
}

func TestInsertAndConsume_FIFO(t *testing.T) {
	repo, _, _ := testutil.NewTestRepo(t)
	ctx := context.Background()

	first := newMessage(t, "orders", "first")
	second := newMessage(t, "orders", "second")
	second.ReceivedAt = first.ReceivedAt + 1 // guarantee ordering

	if err := repo.InsertMessage(first, ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertMessage(second, ctx); err != nil {
		t.Fatal(err)
	}

	msg, err := repo.SelectMessageForConsuming("orders", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || msg.Content != "first" {
		t.Fatalf("expected the oldest message first, got %+v", msg)
	}
	if msg.ProcessingStartedAt == 0 {
		t.Fatal("expected a non-zero delivery receipt (processing_started_at)")
	}

	msg2, err := repo.SelectMessageForConsuming("orders", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if msg2 == nil || msg2.Content != "second" {
		t.Fatalf("expected the second message, got %+v", msg2)
	}

	// queue drained: consuming again finds nothing
	msg3, err := repo.SelectMessageForConsuming("orders", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if msg3 != nil {
		t.Fatalf("expected no message, got %+v", msg3)
	}
}

func TestConsume_ClaimedMessageIsInvisible(t *testing.T) {
	repo, _, _ := testutil.NewTestRepo(t)
	ctx := context.Background()

	if err := repo.InsertMessage(newMessage(t, "orders", "x"), ctx); err != nil {
		t.Fatal(err)
	}

	if msg, _ := repo.SelectMessageForConsuming("orders", ctx); msg == nil {
		t.Fatal("expected a message")
	}
	// same message must not be claimable twice
	if msg, _ := repo.SelectMessageForConsuming("orders", ctx); msg != nil {
		t.Fatalf("claimed message was claimable again: %+v", msg)
	}
}

func TestConsume_DelayedMessageNotVisible(t *testing.T) {
	repo, _, _ := testutil.NewTestRepo(t)
	ctx := context.Background()

	delayed := newMessage(t, "orders", "later")
	delayed.ProcessAfter = time.Now().UnixMilli() + 60_000
	if err := repo.InsertMessage(delayed, ctx); err != nil {
		t.Fatal(err)
	}

	if msg, _ := repo.SelectMessageForConsuming("orders", ctx); msg != nil {
		t.Fatalf("delayed message was delivered early: %+v", msg)
	}
}

func TestAck_ReceiptFencing(t *testing.T) {
	repo, _, _ := testutil.NewTestRepo(t)
	ctx := context.Background()

	if err := repo.InsertMessage(newMessage(t, "orders", "x"), ctx); err != nil {
		t.Fatal(err)
	}
	msg, err := repo.SelectMessageForConsuming("orders", ctx)
	if err != nil || msg == nil {
		t.Fatalf("consume failed: %v %v", err, msg)
	}

	// wrong receipt must not delete the delivery
	err = repo.DeleteMessageOnAck(msg.Id, "orders", msg.ProcessingStartedAt+1, ctx)
	if !errors.Is(err, common.ErrNotFoundMessage) {
		t.Fatalf("ack with wrong receipt: got %v, want ErrNotFoundMessage", err)
	}

	// correct receipt deletes
	if err := repo.DeleteMessageOnAck(msg.Id, "orders", msg.ProcessingStartedAt, ctx); err != nil {
		t.Fatalf("ack with correct receipt failed: %v", err)
	}

	// double ack is a 0-row no-op reported as not found
	err = repo.DeleteMessageOnAck(msg.Id, "orders", msg.ProcessingStartedAt, ctx)
	if !errors.Is(err, common.ErrNotFoundMessage) {
		t.Fatalf("double ack: got %v, want ErrNotFoundMessage", err)
	}
}

func TestNack_BackoffDelays(t *testing.T) {
	repo, appConfigs, rawDB := testutil.NewTestRepo(t)
	ctx := context.Background()

	if err := repo.InsertMessage(newMessage(t, "orders", "x"), ctx); err != nil {
		t.Fatal(err)
	}

	// the claim increments attempts, so failure N arrives with attempts = N;
	// the documented delays are 1s, 5s, 15s, 30s, 60s
	for attempt, wantDelayMs := range appConfigs.BackoffDelaysMs {
		msg, err := repo.SelectMessageForConsuming("orders", ctx)
		if err != nil || msg == nil {
			t.Fatalf("attempt %d: consume failed: %v %v", attempt+1, err, msg)
		}

		err = repo.UpdateMessageOnConsumingFailure(msg.Id, "orders", msg.ProcessingStartedAt, ctx)
		if err != nil {
			t.Fatalf("attempt %d: nack failed: %v", attempt+1, err)
		}

		var status int
		var attempts int
		var delayMs int64
		err = rawDB.QueryRow(
			"SELECT status, attempts, process_after - updated_at FROM messages WHERE id = ?", msg.Id,
		).Scan(&status, &attempts, &delayMs)
		if err != nil {
			t.Fatal(err)
		}

		if attempts != attempt+1 {
			t.Fatalf("attempt %d: attempts = %d", attempt+1, attempts)
		}

		isLastAttempt := attempt == appConfigs.MaxDeliveryAttempts-1
		if isLastAttempt {
			if status != common.FailedStatus {
				t.Fatalf("after %d attempts status = %d, want failed (%d)", attempts, status, common.FailedStatus)
			}
		} else {
			if status != common.ReadyStatus {
				t.Fatalf("attempt %d: status = %d, want ready (%d)", attempt+1, status, common.ReadyStatus)
			}
			if delayMs != wantDelayMs {
				t.Fatalf("attempt %d: backoff = %dms, want %dms", attempt+1, delayMs, wantDelayMs)
			}
			// make the message consumable again for the next round
			if _, err := rawDB.Exec("UPDATE messages SET process_after = ? WHERE id = ?", time.Now().UnixMilli()-1, msg.Id); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestNack_ReceiptFencing(t *testing.T) {
	repo, _, _ := testutil.NewTestRepo(t)
	ctx := context.Background()

	if err := repo.InsertMessage(newMessage(t, "orders", "x"), ctx); err != nil {
		t.Fatal(err)
	}
	msg, _ := repo.SelectMessageForConsuming("orders", ctx)

	err := repo.UpdateMessageOnConsumingFailure(msg.Id, "orders", msg.ProcessingStartedAt+1, ctx)
	if !errors.Is(err, common.ErrNotFoundMessage) {
		t.Fatalf("nack with wrong receipt: got %v, want ErrNotFoundMessage", err)
	}
}

func TestStaleRecovery_FencesLateAck(t *testing.T) {
	repo, appConfigs, rawDB := testutil.NewTestRepo(t)
	ctx := context.Background()

	if err := repo.InsertMessage(newMessage(t, "orders", "x"), ctx); err != nil {
		t.Fatal(err)
	}
	// consumer A claims and then goes silent past the visibility timeout
	msgA, _ := repo.SelectMessageForConsuming("orders", ctx)
	staleTs := time.Now().UnixMilli() - appConfigs.MaxProcessingTimeMs - 1
	if _, err := rawDB.Exec("UPDATE messages SET processing_started_at = ? WHERE id = ?", staleTs, msgA.Id); err != nil {
		t.Fatal(err)
	}

	recovered, err := repo.UpdateStaleMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered %d stale messages, want 1", recovered)
	}

	// the receipt is processing_started_at in ms, so B's claim must land in a
	// later millisecond than A's for the receipts to differ. In production
	// that's guaranteed (redelivery happens >= 1s backoff or >= 5min stale
	// window after the original claim); in the test we compress time, so wait
	// out the millisecond explicitly.
	time.Sleep(5 * time.Millisecond)

	// consumer B claims the redelivery
	msgB, _ := repo.SelectMessageForConsuming("orders", ctx)
	if msgB == nil {
		t.Fatal("expected redelivery after stale recovery")
	}

	// A's late ack carries the receipt it was given (now rewound in the DB by
	// the test, but A never learns that) - it must NOT delete B's delivery
	err = repo.DeleteMessageOnAck(msgA.Id, "orders", msgA.ProcessingStartedAt, ctx)
	if !errors.Is(err, common.ErrNotFoundMessage) {
		t.Fatalf("late ack from timed-out consumer: got %v, want ErrNotFoundMessage", err)
	}

	// B's ack with B's receipt succeeds
	if err := repo.DeleteMessageOnAck(msgB.Id, "orders", msgB.ProcessingStartedAt, ctx); err != nil {
		t.Fatalf("B's ack failed: %v", err)
	}
}

func TestFailedMessages_MoveToDlq(t *testing.T) {
	repo, _, rawDB := testutil.NewTestRepo(t)
	ctx := context.Background()

	msg := newMessage(t, "orders", "doomed")
	if err := repo.InsertMessage(msg, ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec("UPDATE messages SET status = ? WHERE id = ?", common.FailedStatus, msg.Id); err != nil {
		t.Fatal(err)
	}

	moved, err := repo.UpdateFailedMessagesForRegularQueues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if moved != 1 {
		t.Fatalf("moved %d, want 1", moved)
	}

	var queue string
	var isDlq bool
	var attempts int
	var failureReason string
	err = rawDB.QueryRow("SELECT queue, is_dlq, attempts, failure_reason FROM messages WHERE id = ?", msg.Id).
		Scan(&queue, &isDlq, &attempts, &failureReason)
	if err != nil {
		t.Fatal(err)
	}
	if queue != "orders-dlq" || !isDlq || attempts != 0 {
		t.Fatalf("after DLQ move: queue=%s is_dlq=%v attempts=%d", queue, isDlq, attempts)
	}
	if failureReason != common.MaxAttemptsReachedFailureReason {
		t.Fatalf("failure_reason = %q", failureReason)
	}
}

func TestExpiredSweep_BatchesAndSkipsProcessing(t *testing.T) {
	repo, _, rawDB := testutil.NewTestRepo(t)
	ctx := context.Background()

	// seed 2500 expired ready messages (more than two sweep batches)
	// plus one expired-but-processing message that must be left alone
	nowMs := time.Now().UnixMilli()
	tx, err := rawDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO messages (id, queue, content, status, process_after, received_at, updated_at, expires_after)
		VALUES (?, 'orders', 'x', ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2500; i++ {
		id, _ := uuid.NewV7()
		if _, err := stmt.Exec(id.String(), common.ReadyStatus, nowMs, nowMs, nowMs, nowMs-1); err != nil {
			t.Fatal(err)
		}
	}
	processingID, _ := uuid.NewV7()
	if _, err := stmt.Exec(processingID.String(), common.ProcessingStatus, nowMs, nowMs, nowMs, nowMs-1); err != nil {
		t.Fatal(err)
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	moved, err := repo.UpdateExpiredMessagesForRegularQueues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if moved != 2500 {
		t.Fatalf("moved %d expired messages, want 2500", moved)
	}

	var dlqCount int
	if err := rawDB.QueryRow("SELECT COUNT(*) FROM messages WHERE queue = 'orders-dlq' AND is_dlq = TRUE").Scan(&dlqCount); err != nil {
		t.Fatal(err)
	}
	if dlqCount != 2500 {
		t.Fatalf("DLQ has %d messages, want 2500", dlqCount)
	}

	// the processing message stayed in place
	var queue string
	if err := rawDB.QueryRow("SELECT queue FROM messages WHERE id = ?", processingID.String()).Scan(&queue); err != nil {
		t.Fatal(err)
	}
	if queue != "orders" {
		t.Fatalf("processing message was swept into %q", queue)
	}
}

func TestExpiredDlqSweep_Deletes(t *testing.T) {
	repo, _, rawDB := testutil.NewTestRepo(t)
	ctx := context.Background()

	nowMs := time.Now().UnixMilli()
	for i := 0; i < 3; i++ {
		id, _ := uuid.NewV7()
		_, err := rawDB.Exec(`INSERT INTO messages (id, queue, is_dlq, content, status, process_after, received_at, updated_at, expires_after)
			VALUES (?, 'orders-dlq', TRUE, 'x', ?, ?, ?, ?, ?)`,
			id.String(), common.ReadyStatus, nowMs, nowMs, nowMs, nowMs-1)
		if err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := repo.DeleteExpiredMessagesFromDlq(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("deleted %d, want 3", deleted)
	}
}

func TestRequeueDlqMessages(t *testing.T) {
	repo, _, rawDB := testutil.NewTestRepo(t)
	ctx := context.Background()

	nowMs := time.Now().UnixMilli()
	id, _ := uuid.NewV7()
	_, err := rawDB.Exec(`INSERT INTO messages (id, queue, is_dlq, content, status, attempts, process_after, received_at, updated_at, expires_after, failure_reason)
		VALUES (?, 'orders-dlq', TRUE, 'x', ?, 5, ?, ?, ?, ?, 'max_attempts_reached')`,
		id.String(), common.ReadyStatus, nowMs, nowMs, nowMs, nowMs+1000)
	if err != nil {
		t.Fatal(err)
	}

	requeued, err := repo.RequeueDlqMessages("orders-dlq", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if requeued != 1 {
		t.Fatalf("requeued %d, want 1", requeued)
	}

	var queue string
	var isDlq bool
	var attempts int
	var failureReason sql.NullString
	if err := rawDB.QueryRow("SELECT queue, is_dlq, attempts, failure_reason FROM messages WHERE id = ?", id.String()).
		Scan(&queue, &isDlq, &attempts, &failureReason); err != nil {
		t.Fatal(err)
	}
	if queue != "orders" || isDlq || attempts != 0 || failureReason.Valid {
		t.Fatalf("after requeue: queue=%s is_dlq=%v attempts=%d failure_reason=%v", queue, isDlq, attempts, failureReason)
	}
}

func TestSelectMessagesForUI_KeysetPagination(t *testing.T) {
	repo, _, _ := testutil.NewTestRepo(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := repo.InsertMessage(newMessage(t, "orders", fmt.Sprintf("m%d", i)), ctx); err != nil {
			t.Fatal(err)
		}
	}

	page1, err := repo.SelectMessagesForUI("orders", "", 3, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 3 {
		t.Fatalf("page 1 has %d messages, want 3", len(page1))
	}

	page2, err := repo.SelectMessagesForUI("orders", page1[len(page1)-1].Id, 3, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 {
		t.Fatalf("page 2 has %d messages, want 2", len(page2))
	}

	// no overlap between pages
	seen := map[string]bool{}
	for _, m := range append(page1, page2...) {
		if seen[m.Id] {
			t.Fatalf("message %s appears on both pages", m.Id)
		}
		seen[m.Id] = true
	}
}

func TestQueueStats(t *testing.T) {
	repo, _, _ := testutil.NewTestRepo(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := repo.InsertMessage(newMessage(t, "orders", "x"), ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.InsertMessage(newMessage(t, "emails", "x"), ctx); err != nil {
		t.Fatal(err)
	}

	all, err := repo.SelectAllQueuesWithStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d queues, want 2", len(all))
	}

	stats, err := repo.SelectQueueStats("orders", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats == nil || stats.MessagesCount != 3 {
		t.Fatalf("orders stats = %+v, want 3 messages", stats)
	}

	missing, err := repo.SelectQueueStats("nope", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("expected nil stats for unknown queue, got %+v", missing)
	}
}
