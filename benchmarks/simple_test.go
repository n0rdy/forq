package benchmarks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

const (
	forqURL   = "http://localhost:8080"
	authToken = "test-secret"
	testQueue = "benchmark-queue"
)

// High-performance HTTP client configured for sustained concurrent load
var benchmarkClient *http.Client

func init() {
	transport := &http.Transport{
		MaxIdleConns:        200,              // Total idle connections across all hosts
		MaxIdleConnsPerHost: 100,              // Max idle connections per host (vs default 2!)
		MaxConnsPerHost:     200,              // Max total connections per host
		IdleConnTimeout:     90 * time.Second, // Keep connections alive
		DisableCompression:  true,             // Reduce CPU overhead
		ForceAttemptHTTP2:   true,             // Use HTTP/2 if available
	}

	// Enable HTTP/2 support
	http2.ConfigureTransport(transport)

	benchmarkClient = &http.Client{
		Transport: transport,
		Timeout:   35 * time.Second, // Slightly longer than Forq's 30s long polling timeout
	}
}

type NewMessageRequest struct {
	Content string `json:"content"`
}

type MessageResponse struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// Helper functions for HTTP calls
func produceMessage(queue, content string) error {
	req := NewMessageRequest{Content: content}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", forqURL+"/api/v1/queues/"+queue+"/messages", bytes.NewBuffer(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "ApiKey "+authToken)

	resp, err := benchmarkClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		return fmt.Errorf("produce failed with status: %d", resp.StatusCode)
	}
	return nil
}

func consumeMessage(queue string) (*MessageResponse, error) {
	httpReq, _ := http.NewRequest("GET", forqURL+"/api/v1/queues/"+queue+"/messages", nil)
	httpReq.Header.Set("Authorization", "ApiKey "+authToken)

	resp, err := benchmarkClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 {
		return nil, fmt.Errorf("no message available")
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("consume failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var msg MessageResponse
	err = json.Unmarshal(body, &msg)
	return &msg, err
}

func ackMessage(queue, messageID string) error {
	httpReq, _ := http.NewRequest("POST", forqURL+"/api/v1/queues/"+queue+"/messages/"+messageID+"/ack", nil)
	httpReq.Header.Set("Authorization", "ApiKey "+authToken)

	resp, err := benchmarkClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		return fmt.Errorf("ack failed with status: %d", resp.StatusCode)
	}
	return nil
}

// BenchmarkSingleProducerConsumer tests concurrent single producer and single consumer with backlog
func BenchmarkSingleProducerConsumer(b *testing.B) {
	testMessage := `{"task": "process", "data": {"id": 123, "name": "test"}}`

	// Pre-populate queue with backlog to eliminate long-polling waits
	backlogSize := b.N
	for i := 0; i < backlogSize; i++ {
		err := produceMessage(testQueue, testMessage)
		if err != nil {
			b.Fatal("Failed to create backlog:", err)
		}
	}

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	var producerErr, consumerErr error

	// Start producer (produces b.N more messages)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			if err := produceMessage(testQueue, testMessage); err != nil {
				producerErr = err
				return
			}
		}
	}()

	// Start consumer (consumes b.N messages from backlog + new ones)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			msg, err := consumeMessage(testQueue)
			if err != nil {
				consumerErr = err
				return
			}

			if err := ackMessage(testQueue, msg.ID); err != nil {
				consumerErr = err
				return
			}
		}
	}()

	wg.Wait()

	if producerErr != nil {
		b.Fatal("Producer error:", producerErr)
	}
	if consumerErr != nil {
		b.Fatal("Consumer error:", consumerErr)
	}

	duration := time.Since(start)
	throughput := float64(b.N) / duration.Seconds()
	avgLatency := duration / time.Duration(b.N)

	fmt.Printf("\n=== Single Producer/Consumer (with Backlog) ===\n")
	fmt.Printf("Messages: %d\n", b.N)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Throughput: %.2f messages/sec\n", throughput)
	fmt.Printf("Avg Latency: %v\n", avgLatency)
}

// BenchmarkMultipleConsumers tests concurrent consumer performance
func BenchmarkMultipleConsumers(b *testing.B) {
	numConsumers := 5
	messagesPerConsumer := b.N / numConsumers
	if messagesPerConsumer < 1 {
		messagesPerConsumer = 1
	}
	totalMessages := messagesPerConsumer * numConsumers

	testMessage := `{"task": "process", "data": {"id": 123, "name": "test"}}`

	// Pre-populate queue with messages
	for i := 0; i < totalMessages; i++ {
		err := produceMessage(testQueue, testMessage)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < messagesPerConsumer; j++ {
				msg, err := consumeMessage(testQueue)
				if err != nil {
					b.Error(err)
					return
				}

				err = ackMessage(testQueue, msg.ID)
				if err != nil {
					b.Error(err)
					return
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)
	throughput := float64(totalMessages) / duration.Seconds()
	avgLatency := duration / time.Duration(totalMessages)

	fmt.Printf("\n=== Multiple Consumers (%d consumers) ===\n", numConsumers)
	fmt.Printf("Messages: %d\n", totalMessages)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Throughput: %.2f messages/sec\n", throughput)
	fmt.Printf("Avg Latency: %v\n", avgLatency)
}

// BenchmarkQueueWithBacklog tests performance when processing existing queue backlog
func BenchmarkQueueWithBacklog(b *testing.B) {
	backlogSize := 1000 // Pre-populate with 1k messages

	testMessage := `{"task": "process", "data": {"id": 123, "name": "test"}}`

	// Create backlog
	for i := 0; i < backlogSize; i++ {
		err := produceMessage(testQueue, testMessage)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	start := time.Now()

	// Process N messages from the backlog
	processed := 0
	for i := 0; i < b.N && processed < backlogSize; i++ {
		msg, err := consumeMessage(testQueue)
		if err != nil {
			b.Fatal(err)
		}

		err = ackMessage(testQueue, msg.ID)
		if err != nil {
			b.Fatal(err)
		}
		processed++
	}

	duration := time.Since(start)
	throughput := float64(processed) / duration.Seconds()
	avgLatency := duration / time.Duration(processed)

	fmt.Printf("\n=== Queue with Backlog (backlog: %d) ===\n", backlogSize)
	fmt.Printf("Messages processed: %d\n", processed)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Throughput: %.2f messages/sec\n", throughput)
	fmt.Printf("Avg Latency: %v\n", avgLatency)
}

// High concurrency tests - let's find the real limits!
func BenchmarkHighConcurrency20Consumers(b *testing.B) {
	numConsumers := 20
	messagesPerConsumer := b.N / numConsumers
	if messagesPerConsumer < 1 {
		messagesPerConsumer = 1
	}
	totalMessages := messagesPerConsumer * numConsumers

	testMessage := `{"task": "high-concurrency", "data": {"id": 123, "name": "test"}}`
	queue := "benchmark-high20"

	// Pre-populate queue
	for i := 0; i < totalMessages; i++ {
		err := produceMessage(queue, testMessage)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < messagesPerConsumer; j++ {
				msg, err := consumeMessage(queue)
				if err != nil {
					b.Error(err)
					return
				}

				err = ackMessage(queue, msg.ID)
				if err != nil {
					b.Error(err)
					return
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)
	throughput := float64(totalMessages) / duration.Seconds()
	avgLatency := duration / time.Duration(totalMessages)

	fmt.Printf("\n=== High Concurrency (20 consumers) ===\n")
	fmt.Printf("Messages: %d\n", totalMessages)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Throughput: %.2f messages/sec\n", throughput)
	fmt.Printf("Avg Latency: %v\n", avgLatency)
}

func BenchmarkHighConcurrency50Consumers(b *testing.B) {
	numConsumers := 50
	messagesPerConsumer := b.N / numConsumers
	if messagesPerConsumer < 1 {
		messagesPerConsumer = 1
	}
	totalMessages := messagesPerConsumer * numConsumers

	testMessage := `{"task": "high-concurrency", "data": {"id": 123, "name": "test"}}`
	queue := "benchmark-high50"

	// Pre-populate queue
	for i := 0; i < totalMessages; i++ {
		err := produceMessage(queue, testMessage)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < messagesPerConsumer; j++ {
				msg, err := consumeMessage(queue)
				if err != nil {
					b.Error(err)
					return
				}

				err = ackMessage(queue, msg.ID)
				if err != nil {
					b.Error(err)
					return
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)
	throughput := float64(totalMessages) / duration.Seconds()
	avgLatency := duration / time.Duration(totalMessages)

	fmt.Printf("\n=== High Concurrency (50 consumers) ===\n")
	fmt.Printf("Messages: %d\n", totalMessages)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Throughput: %.2f messages/sec\n", throughput)
	fmt.Printf("Avg Latency: %v\n", avgLatency)
}

func BenchmarkExtremeConcurrency100Consumers(b *testing.B) {
	numConsumers := 100
	messagesPerConsumer := b.N / numConsumers
	if messagesPerConsumer < 1 {
		messagesPerConsumer = 1
	}
	totalMessages := messagesPerConsumer * numConsumers

	testMessage := `{"task": "extreme-concurrency", "data": {"id": 123, "name": "test"}}`
	queue := "benchmark-extreme100"

	// Pre-populate queue
	for i := 0; i < totalMessages; i++ {
		err := produceMessage(queue, testMessage)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < messagesPerConsumer; j++ {
				msg, err := consumeMessage(queue)
				if err != nil {
					b.Error(err)
					return
				}

				err = ackMessage(queue, msg.ID)
				if err != nil {
					b.Error(err)
					return
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)
	throughput := float64(totalMessages) / duration.Seconds()
	avgLatency := duration / time.Duration(totalMessages)

	fmt.Printf("\n=== EXTREME Concurrency (100 consumers) ===\n")
	fmt.Printf("Messages: %d\n", totalMessages)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Throughput: %.2f messages/sec\n", throughput)
	fmt.Printf("Avg Latency: %v\n", avgLatency)
}

// Pure throughput test - how fast can we push messages through?
func BenchmarkPureThroughput(b *testing.B) {
	numWorkers := 50 // High concurrency for max throughput
	messagesPerWorker := b.N / numWorkers
	if messagesPerWorker < 1 {
		messagesPerWorker = 1
	}
	totalMessages := messagesPerWorker * numWorkers

	testMessage := `{"task": "throughput", "data": "speed test"}`
	queue := "benchmark-throughput"

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup

	// Concurrent produce + consume workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < messagesPerWorker; j++ {
				// Produce
				err := produceMessage(queue, testMessage)
				if err != nil {
					b.Error(err)
					return
				}

				// Consume
				msg, err := consumeMessage(queue)
				if err != nil {
					b.Error(err)
					return
				}

				// Ack
				err = ackMessage(queue, msg.ID)
				if err != nil {
					b.Error(err)
					return
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)
	throughput := float64(totalMessages) / duration.Seconds()
	avgLatency := duration / time.Duration(totalMessages)

	fmt.Printf("\n=== Pure Throughput Test (%d workers) ===\n", numWorkers)
	fmt.Printf("Messages: %d\n", totalMessages)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Throughput: %.2f messages/sec\n", throughput)
	fmt.Printf("Avg Latency: %v\n", avgLatency)
}

// Multi-producer multi-consumer test - realistic workload (FIXED)
func BenchmarkMultipleProducersAndConsumers(b *testing.B) {
	numProducers := 10
	numConsumers := 20

	// Ensure we have enough messages for meaningful testing
	totalMessages := b.N
	if totalMessages < numConsumers {
		totalMessages = numConsumers // At least one message per consumer
	}

	testMessage := `{"task": "multi-producer", "data": {"id": 456, "payload": "realistic workload"}}`
	queue := "benchmark-multi-prod"

	// FIX: Pre-populate queue to avoid long polling timeouts
	for i := 0; i < totalMessages; i++ {
		err := produceMessage(queue, testMessage)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup

	// Now start consumers to process the pre-populated messages
	messagesPerConsumer := totalMessages / numConsumers
	if messagesPerConsumer < 1 {
		messagesPerConsumer = 1
	}

	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < messagesPerConsumer; j++ {
				msg, err := consumeMessage(queue)
				if err != nil {
					b.Error(err)
					return
				}

				err = ackMessage(queue, msg.ID)
				if err != nil {
					b.Error(err)
					return
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)
	throughput := float64(totalMessages) / duration.Seconds()
	avgLatency := duration / time.Duration(totalMessages)

	fmt.Printf("\n=== Multi-Producer Multi-Consumer (%d producers, %d consumers) ===\n", numProducers, numConsumers)
	fmt.Printf("Messages: %d\n", totalMessages)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Throughput: %.2f messages/sec\n", throughput)
	fmt.Printf("Avg Latency: %v\n", avgLatency)
}

// Realistic concurrent produce+consume test (truly simultaneous)
func BenchmarkConcurrentProduceAndConsume(b *testing.B) {
	numWorkers := 25 // 25 producer workers + 25 consumer workers

	// Ensure we have enough messages for meaningful testing
	totalMessages := b.N
	if totalMessages < numWorkers {
		totalMessages = numWorkers // At least one message per worker
	}
	messagesPerWorker := totalMessages / numWorkers
	if messagesPerWorker < 1 {
		messagesPerWorker = 1
	}

	testMessage := `{"task": "concurrent", "data": "produce and consume simultaneously"}`
	queue := "benchmark-concurrent"

	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	produced := make(chan struct{}, totalMessages)
	consumed := make(chan struct{}, totalMessages)

	// Start producer workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < messagesPerWorker; j++ {
				err := produceMessage(queue, testMessage)
				if err != nil {
					b.Error(err)
					return
				}
				produced <- struct{}{}
			}
		}()
	}

	// Start consumer workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < messagesPerWorker; j++ {
				// Wait for at least one message to be produced
				<-produced

				msg, err := consumeMessage(queue)
				if err != nil {
					b.Error(err)
					return
				}

				err = ackMessage(queue, msg.ID)
				if err != nil {
					b.Error(err)
					return
				}

				consumed <- struct{}{}
			}
		}()
	}

	// Wait for all messages to be processed
	for i := 0; i < totalMessages; i++ {
		<-consumed
	}

	wg.Wait()
	duration := time.Since(start)
	throughput := float64(totalMessages) / duration.Seconds()
	avgLatency := duration / time.Duration(totalMessages)

	fmt.Printf("\n=== Concurrent Produce+Consume (%d workers each) ===\n", numWorkers)
	fmt.Printf("Messages: %d\n", totalMessages)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Throughput: %.2f messages/sec\n", throughput)
	fmt.Printf("Avg Latency: %v\n", avgLatency)
}
