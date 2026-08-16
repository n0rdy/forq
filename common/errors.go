package common

const (
	ErrCodeBadRequestContentExceedsLimit = "bad_request.body.content.exceeds_limit"
	ErrCodeBadRequestProcessAfterInPast  = "bad_request.body.processAfter.in_past"
	ErrCodeBadRequestProcessAfterTooFar  = "bad_request.body.processAfter.too_far"
	ErrCodeBadRequestInvalidBody         = "bad_request.body.invalid"
	ErrCodeBadRequestInvalidQueueName    = "bad_request.queue.invalid_name"
	ErrCodeBadRequestInvalidMessageId    = "bad_request.messageId.invalid"
	ErrCodeBadRequestProduceToDlq        = "bad_request.queue.produce_to_dlq"
	ErrCodeBadRequestDlqOnlyOp           = "bad_request.dlq_only_operation"
	ErrCodeBadRequestReceiptMissing      = "bad_request.receipt.missing"
	ErrCodeBadRequestReceiptInvalid      = "bad_request.receipt.invalid"
	ErrCodeUnauthorized                  = "unauthorized"
	ErrCodeTooManyRequests               = "too_many_requests"
	ErrCodeNotFoundMessage               = "not_found.message"
	ErrCodeServiceUnhealthy              = "forq.unhealthy"
	ErrCodeInternal                      = "internal"
)

var (
	ErrBadRequestContentExceedsLimit = ForqError{Code: ErrCodeBadRequestContentExceedsLimit}
	ErrBadRequestProcessAfterInPast  = ForqError{Code: ErrCodeBadRequestProcessAfterInPast}
	ErrBadRequestProcessAfterTooFar  = ForqError{Code: ErrCodeBadRequestProcessAfterTooFar}
	ErrBadRequestInvalidQueueName    = ForqError{Code: ErrCodeBadRequestInvalidQueueName}
	ErrBadRequestInvalidMessageId    = ForqError{Code: ErrCodeBadRequestInvalidMessageId}
	ErrBadRequestProduceToDlq        = ForqError{Code: ErrCodeBadRequestProduceToDlq}
	ErrBadRequestDlqOnlyOp           = ForqError{Code: ErrCodeBadRequestDlqOnlyOp}
	ErrBadRequestReceiptMissing      = ForqError{Code: ErrCodeBadRequestReceiptMissing}
	ErrBadRequestReceiptInvalid      = ForqError{Code: ErrCodeBadRequestReceiptInvalid}
	ErrNotFoundMessage               = ForqError{Code: ErrCodeNotFoundMessage}
	ErrInternal                      = ForqError{Code: ErrCodeInternal}
)

type ForqError struct {
	Code string
}

func (fe ForqError) Error() string {
	return fe.Code
}
