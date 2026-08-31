package common

import (
	"regexp"
	"uuid"
)

// queueNameRegex bounds queue names to a safe charset and length. Anything
// outside it would end up as an unescaped URL segment in the admin UI and an
// unbounded Prometheus label, so validation happens at the API boundary.
var queueNameRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

func IsValidQueueName(name string) bool {
	return queueNameRegex.MatchString(name)
}

func IsValidMessageId(messageId string) bool {
	_, err := uuid.Parse(messageId)
	return err == nil
}
