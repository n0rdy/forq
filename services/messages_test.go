package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/n0rdy/forq/common"
	"github.com/n0rdy/forq/internal/testutil"
	"github.com/n0rdy/forq/metrics"
	"github.com/n0rdy/forq/services"
)

func newMessagesService(t *testing.T) *services.MessagesService {
	t.Helper()
	repo, appConfigs, _ := testutil.NewTestRepo(t)
	// metrics disabled -> noop implementation, avoids duplicate Prometheus
	// registration across tests
	return services.NewMessagesService(metrics.NewMetricsService(false), repo, appConfigs)
}

func TestProcessNewMessage_Validation(t *testing.T) {
	svc := newMessagesService(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		msg     common.NewMessageRequest
		queue   string
		wantErr error
	}{
		{
			name:  "valid message",
			msg:   common.NewMessageRequest{Content: "hello"},
			queue: "orders",
		},
		{
			name:  "valid delayed message",
			msg:   common.NewMessageRequest{Content: "later", ProcessAfter: time.Now().UnixMilli() + 60_000},
			queue: "orders",
		},
		{
			name:    "content exceeds 256KB",
			msg:     common.NewMessageRequest{Content: strings.Repeat("x", 256*1024+1)},
			queue:   "orders",
			wantErr: common.ErrBadRequestContentExceedsLimit,
		},
		{
			name:    "processAfter in the past",
			msg:     common.NewMessageRequest{Content: "x", ProcessAfter: time.Now().UnixMilli() - 60_000},
			queue:   "orders",
			wantErr: common.ErrBadRequestProcessAfterInPast,
		},
		{
			name:    "processAfter too far in the future",
			msg:     common.NewMessageRequest{Content: "x", ProcessAfter: time.Now().UnixMilli() + 400*24*60*60*1000},
			queue:   "orders",
			wantErr: common.ErrBadRequestProcessAfterTooFar,
		},
		{
			name:    "producing directly into a DLQ is rejected",
			msg:     common.NewMessageRequest{Content: "x"},
			queue:   "orders-dlq",
			wantErr: common.ErrBadRequestProduceToDlq,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.ProcessNewMessage(tt.msg, tt.queue, ctx)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestConsume_ReturnsOpaqueReceipt(t *testing.T) {
	svc := newMessagesService(t)
	ctx := context.Background()

	if err := svc.ProcessNewMessage(common.NewMessageRequest{Content: "hello"}, "orders", ctx); err != nil {
		t.Fatal(err)
	}

	msg, err := svc.GetMessageForConsuming("orders", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil {
		t.Fatal("expected a message")
	}
	if msg.Receipt == "" {
		t.Fatal("expected a non-empty receipt")
	}
	if msg.Content != "hello" {
		t.Fatalf("content = %q", msg.Content)
	}
}

func TestConsume_ClientDisconnectIsNotAnError(t *testing.T) {
	svc := newMessagesService(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	msg, err := svc.GetMessageForConsuming("empty-queue", ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("client disconnect returned error: %v", err)
	}
	if msg != nil {
		t.Fatalf("expected no message, got %+v", msg)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("poll did not return promptly on disconnect (%v)", elapsed)
	}
}

func TestAckNack_ReceiptRequired(t *testing.T) {
	svc := newMessagesService(t)
	ctx := context.Background()

	if err := svc.ProcessNewMessage(common.NewMessageRequest{Content: "x"}, "orders", ctx); err != nil {
		t.Fatal(err)
	}
	msg, err := svc.GetMessageForConsuming("orders", ctx)
	if err != nil || msg == nil {
		t.Fatalf("consume failed: %v %v", err, msg)
	}

	if err := svc.AckMessage(msg.Id, "orders", "", ctx); !errors.Is(err, common.ErrBadRequestReceiptMissing) {
		t.Fatalf("ack without receipt: got %v, want ErrBadRequestReceiptMissing", err)
	}
	if err := svc.AckMessage(msg.Id, "orders", "not-a-number", ctx); !errors.Is(err, common.ErrBadRequestReceiptInvalid) {
		t.Fatalf("ack with garbage receipt: got %v, want ErrBadRequestReceiptInvalid", err)
	}
	if err := svc.NackMessage(msg.Id, "orders", "", ctx); !errors.Is(err, common.ErrBadRequestReceiptMissing) {
		t.Fatalf("nack without receipt: got %v, want ErrBadRequestReceiptMissing", err)
	}

	if err := svc.AckMessage(msg.Id, "orders", msg.Receipt, ctx); err != nil {
		t.Fatalf("ack with valid receipt failed: %v", err)
	}
}

func TestRequeueAndDelete_DlqOnly(t *testing.T) {
	svc := newMessagesService(t)
	ctx := context.Background()

	if err := svc.RequeueAllDlqMessages("orders", ctx); !errors.Is(err, common.ErrBadRequestDlqOnlyOp) {
		t.Fatalf("requeue-all on non-DLQ: got %v, want ErrBadRequestDlqOnlyOp", err)
	}
	if err := svc.DeleteAllDlqMessages("orders", ctx); !errors.Is(err, common.ErrBadRequestDlqOnlyOp) {
		t.Fatalf("delete-all on non-DLQ: got %v, want ErrBadRequestDlqOnlyOp", err)
	}
	if err := svc.DeleteDlqMessage("0199164b-4dea-78d9-9b4c-c699d5037962", "orders", ctx); !errors.Is(err, common.ErrBadRequestDlqOnlyOp) {
		t.Fatalf("delete message on non-DLQ: got %v, want ErrBadRequestDlqOnlyOp", err)
	}
}

func TestGetMessagesForUI_Pagination(t *testing.T) {
	svc := newMessagesService(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := svc.ProcessNewMessage(common.NewMessageRequest{Content: "x"}, "orders", ctx); err != nil {
			t.Fatal(err)
		}
	}

	page, err := svc.GetMessagesForUI("orders", "", 3, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 3 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("page 1: %d messages, hasMore=%v, cursor=%q", len(page.Messages), page.HasMore, page.NextCursor)
	}

	page2, err := svc.GetMessagesForUI("orders", page.NextCursor, 3, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Messages) != 2 || page2.HasMore {
		t.Fatalf("page 2: %d messages, hasMore=%v", len(page2.Messages), page2.HasMore)
	}
}
