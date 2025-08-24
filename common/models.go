package common

// LoginPageData contains data for the login page
type LoginPageData struct {
	Title string
	Error string
}

// DashboardPageData contains data for the dashboard page
type DashboardPageData struct {
	Title         string
	TotalQueues   int
	TotalMessages int
	DLQMessages   int
	Queues        []QueueStats
}

// QueuePageData contains data for individual queue pages (queue stats only, no messages)
type QueuePageData struct {
	Title string
	Queue *QueueStats
}

// MessagesComponentData contains data for the messages component with cursor-based pagination
type MessagesComponentData struct {
	Messages   []MessageMetadata
	NextCursor string // Last message ID for cursor-based pagination
	HasMore    bool   // Whether there are more messages to load
	QueueName  string // For HTMX URLs
	IsDLQ      bool   // Whether this is a DLQ queue (for action buttons)
}

// QueueStats represents queue statistics for dashboard display
type QueueStats struct {
	Name          string
	TotalMessages int
	Type          string // "Regular" or "DLQ"
}

// MessageMetadata represents basic metadata about a message with the idea of saving memory and network by not including full content
type MessageMetadata struct {
	ID           string
	Status       string
	Attempts     int
	Age          string
	ProcessAfter string
}

// MessageDetails represents detailed information about a message for UI display (full expansion)
type MessageDetails struct {
	ID                  string
	Content             string
	Status              string
	Attempts            int
	ReceivedAt          string
	Age                 string
	ProcessAfter        string
	ProcessingStartedAt string
	FailureReason       string
	UpdatedAt           string
}
