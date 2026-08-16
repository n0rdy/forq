package db

type NewMessage struct {
	Id           string
	QueueName    string
	Content      string
	ProcessAfter int64
	ReceivedAt   int64
	UpdatedAt    int64
	ExpiresAfter int64
}

type MessageForConsuming struct {
	Id      string
	Content string
	// ProcessingStartedAt fences this delivery: it is returned to the consumer
	// as the receipt and must match on ack/nack.
	ProcessingStartedAt int64
}

type MessageMetadata struct {
	Id           string
	Status       int
	Attempts     int
	ReceivedAt   int64
	ProcessAfter int64
}

type MessageDetails struct {
	Id                  string
	Content             string
	Status              int
	Attempts            int
	ProcessAfter        int64
	ProcessingStartedAt *int64
	FailureReason       *string
	ReceivedAt          int64
	UpdatedAt           int64
	ExpiresAfter        int64
}

type QueueMetadata struct {
	Name          string
	MessagesCount int
	IsDLQ         bool
}
