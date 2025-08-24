package ui

type TemplateData struct {
	Title string
	Error string
	// Dashboard specific data
	Queues        []QueueStats
	TotalQueues   int
	TotalMessages int
	Processing    int
	FailedDLQ     int
	// Queue details specific data
	Queue       *QueueDetails
	Messages    []MessageDetails
	CurrentPage int
	HasNextPage bool
	IsLastPage  bool
}

type QueueStats struct {
	Name          string
	TotalMessages int
	Ready         int
	Processing    int
	Failed        int
}

type QueueDetails struct {
	Name          string
	Type          string // "Regular" or "DLQ"
	IsDLQ         bool
	TotalMessages int
	OldestMessage *MessageSummary
	NewestMessage *MessageSummary
}

type MessageSummary struct {
	ID         string
	ReceivedAt string
	Age        string
}

type MessageDetails struct {
	ID            string
	Content       string
	Status        string
	Attempts      int
	ReceivedAt    string
	Age           string
	ProcessAfter  string
	FailureReason string
}
