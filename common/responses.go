package common

type MessageResponse struct {
	Id      string `json:"id"`
	Content string `json:"content"`
	// Receipt identifies this particular delivery of the message. It must be
	// echoed back on ack/nack (X-Forq-Receipt header) so that a late ack/nack
	// from a consumer that exceeded the visibility timeout can't affect a
	// redelivery claimed by another consumer. Opaque to clients.
	Receipt string `json:"receipt"`
}

type ErrorResponse struct {
	Code string `json:"code,omitempty"`
}
