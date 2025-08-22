package common

type MessageResponse struct {
	Id      string
	Content string
}

type ErrorResponse struct {
	Code string `json:"code,omitempty"`
}
