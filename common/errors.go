package common

const (
	ErrCodeBadRequestContentExceedsLimit = "bad_request.body.content.exceeds_limit"
	ErrCodeBadRequestProcessAfterInPast  = "bad_request.body.processAfter.in_past"
	ErrCodeBadRequestProcessAfterTooFar  = "bad_request.body.processAfter.too_far"
	ErrCodeBadRequestInvalidBody         = "bad_request.body.invalid"
	ErrCodeUnauthorized                  = "unauthorized"
	ErrCodeNotFoundMessage               = "not_found.message"
	ErrCodeInternal                      = "internal"
)

var (
	ErrBadRequestContentExceedsLimit = ForqError{Code: ErrCodeBadRequestContentExceedsLimit}
	ErrBadRequestProcessAfterInPast  = ForqError{Code: ErrCodeBadRequestProcessAfterInPast}
	ErrBadRequestProcessAfterTooFar  = ForqError{Code: ErrCodeBadRequestProcessAfterTooFar}
	ErrNotFoundMessage               = ForqError{Code: ErrCodeNotFoundMessage}
	ErrInternal                      = ForqError{Code: ErrCodeInternal}
)

type ForqError struct {
	Code string
}

func (fe ForqError) Error() string {
	return fe.Code
}
