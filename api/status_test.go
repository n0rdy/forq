package api

import (
	"net/http"
	"testing"

	"github.com/n0rdy/forq/common"
)

// pins the complete error-code -> HTTP status mapping, so no code can
// silently fall through to 500
func TestHttpStatusForErrorCode(t *testing.T) {
	tests := map[string]int{
		common.ErrCodeBadRequestContentExceedsLimit: http.StatusBadRequest,
		common.ErrCodeBadRequestProcessAfterInPast:  http.StatusBadRequest,
		common.ErrCodeBadRequestProcessAfterTooFar:  http.StatusBadRequest,
		common.ErrCodeBadRequestInvalidBody:         http.StatusBadRequest,
		common.ErrCodeBadRequestInvalidQueueName:    http.StatusBadRequest,
		common.ErrCodeBadRequestInvalidMessageId:    http.StatusBadRequest,
		common.ErrCodeBadRequestProduceToDlq:        http.StatusBadRequest,
		common.ErrCodeBadRequestDlqOnlyOp:           http.StatusBadRequest,
		common.ErrCodeBadRequestReceiptMissing:      http.StatusBadRequest,
		common.ErrCodeBadRequestReceiptInvalid:      http.StatusBadRequest,
		common.ErrCodeUnauthorized:                  http.StatusUnauthorized,
		common.ErrCodeTooManyRequests:               http.StatusTooManyRequests,
		common.ErrCodeNotFoundMessage:               http.StatusNotFound,
		common.ErrCodeServiceUnhealthy:              http.StatusServiceUnavailable,
		common.ErrCodeInternal:                      http.StatusInternalServerError,
		"some.unknown.code":                         http.StatusInternalServerError,
	}

	for code, want := range tests {
		if got := httpStatusForErrorCode(code); got != want {
			t.Errorf("httpStatusForErrorCode(%q) = %d, want %d", code, got, want)
		}
	}
}
