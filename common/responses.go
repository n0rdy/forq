package common

type MessageResponse struct {
	Id      string `json:"id"`
	Content string `json:"content"`
}

type ErrorResponse struct {
	Code string `json:"code,omitempty"`
}
