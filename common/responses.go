package common

type MessageResponse struct {
	Id      string `json:"id"`
	Content string `json:"content"`
	// Receipt identifies this particular delivery of the message. It must be
	// echoed back on ack/nack (X-Forq-Receipt header): it fences a stale late
	// ack/nack from a consumer that overran the visibility timeout (its receipt
	// no longer matches the redelivery) against a timing race. It is the claim
	// timestamp, not an unguessable secret - a correctness lease, not a boundary
	// between mutually-distrusting holders of the shared API key. Opaque to
	// clients: treat as a string, don't parse it.
	Receipt string `json:"receipt"`
}

type ErrorResponse struct {
	Code string `json:"code,omitempty"`
}
