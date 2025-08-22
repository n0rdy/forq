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
}

type QueueMetadata struct {
	Name          string
	MessagesCount int
}
