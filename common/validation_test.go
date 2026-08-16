package common

import "testing"

func TestIsValidQueueName(t *testing.T) {
	valid := []string{
		"emails",
		"my-queue",
		"my_queue",
		"queue.v2",
		"Q",
		"1",
		"emails-dlq",
		"a1234567890123456789012345678901234567890123456789012345678901-a", // 64 chars
	}
	for _, name := range valid {
		if !IsValidQueueName(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}

	invalid := []string{
		"",
		"queue with spaces",
		"queue#hash",
		"queue?query",
		"queue/slash",
		"queue%encoded",
		"émails",
		"очередь",
		"a12345678901234567890123456789012345678901234567890123456789012-a", // 65 chars
	}
	for _, name := range invalid {
		if IsValidQueueName(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func TestIsValidMessageId(t *testing.T) {
	valid := []string{
		"0199164b-4dea-78d9-9b4c-c699d5037962", // UUID v7
		"c56a4180-65aa-42ec-a945-5fd21dec0538", // UUID v4
	}
	for _, id := range valid {
		if !IsValidMessageId(id) {
			t.Errorf("expected %q to be valid", id)
		}
	}

	invalid := []string{
		"",
		"not-a-uuid",
		"0199164b-4dea-78d9-9b4c",
		"0199164b4dea78d99b4cc699d5037962zzzz",
	}
	for _, id := range invalid {
		if IsValidMessageId(id) {
			t.Errorf("expected %q to be invalid", id)
		}
	}
}
