package common

type NewMessageRequest struct {
	Content      string `json:"content"`
	ProcessAfter int64  `json:"processAfter,omitempty"` // optional Unix timestamp in milliseconds
}
