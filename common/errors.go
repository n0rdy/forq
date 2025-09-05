package common

const (
	ErrCodeBadRequestContentExceedsLimit = "bad_request.body.content.exceeds_limit"
	ErrCodeBadRequestProcessAfterInPast  = "bad_request.body.processAfter.in_past"
	ErrCodeBadRequestProcessAfterTooFar  = "bad_request.body.processAfter.too_far"
	ErrCodeBadRequestInvalidBody         = "bad_request.body.invalid"
	ErrCodeBadRequestDlqOnlyOp           = "bad_request.dlq_only_operation"
	ErrCodeUnauthorized                  = "unauthorized"
	ErrCodeNotFoundMessage               = "not_found.message"
	ErrCodeServiceUnhealthy              = "forq.unhealthy"
	ErrCodeInternal                      = "internal"
)

var (
	ErrBadRequestContentExceedsLimit = ForqError{Code: ErrCodeBadRequestContentExceedsLimit}
	ErrBadRequestProcessAfterInPast  = ForqError{Code: ErrCodeBadRequestProcessAfterInPast}
	ErrBadRequestProcessAfterTooFar  = ForqError{Code: ErrCodeBadRequestProcessAfterTooFar}
	ErrBadRequestDlqOnlyOp           = ForqError{Code: ErrCodeBadRequestDlqOnlyOp}
	ErrNotFoundMessage               = ForqError{Code: ErrCodeNotFoundMessage}
	ErrInternal                      = ForqError{Code: ErrCodeInternal}
)

type ForqError struct {
	Code string
}

func (fe ForqError) Error() string {
	return fe.Code
}
