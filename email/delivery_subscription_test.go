package email

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/blockysource/blocky-cloud/bcerrors"
	"github.com/blockysource/blocky-cloud/email/batcher"
	"github.com/blockysource/blocky-cloud/email/driver"
)

type driverTopic struct {
	subs []*driverSub
}

func (t *driverTopic) SendBatch(ctx context.Context, ms []*driver.DeliveryEvent) error {
	for _, s := range t.subs {
		select {
		case <-s.sem:
			s.q = append(s.q, ms...)
			s.sem <- struct{}{}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

type driverSub struct {
	driver.DeliverySubscription
	sem chan struct{}
	// Normally this queue would live on a separate server in the cloud.
	q []*driver.DeliveryEvent
}

func NewDriverSub() *driverSub {
	ds := &driverSub{sem: make(chan struct{}, 1)}
	ds.sem <- struct{}{}
	return ds
}

func (s *driverSub) ReceiveBatch(ctx context.Context, maxMessages int) ([]*driver.DeliveryEvent, error) {
	for {
		select {
		case <-s.sem:
			ms := s.grabQueue(maxMessages)
			if len(ms) != 0 {
				return ms, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}

func (s *driverSub) grabQueue(maxMessages int) []*driver.DeliveryEvent {
	defer func() { s.sem <- struct{}{} }()
	if len(s.q) > 0 {
		if len(s.q) <= maxMessages {
			ms := s.q
			s.q = nil
			return ms
		}
		ms := s.q[:maxMessages]
		s.q = s.q[maxMessages:]
		return ms
	}
	return nil
}

func (*driverSub) IsRetryable(error) bool     { return false }
func (*driverSub) ErrorCode(error) codes.Code { return codes.Internal }
func (*driverSub) Close() error               { return nil }

func TestConcurrentReceivesGetAllTheMessages(t *testing.T) {
	howManyToSend := int(1e3)
	ctx, cancel := context.WithCancel(context.Background())

	// wg is used to wait until all messages are received.
	var wg sync.WaitGroup
	wg.Add(howManyToSend)

	// Make a subscription.
	ds := NewDriverSub()
	s := NewDeliverySubscription(ds, nil)
	defer s.Shutdown(ctx)

	dt := &driverTopic{
		subs: []*driverSub{ds},
	}

	// Start 10 goroutines to receive from it.
	var mu sync.Mutex
	receivedMsgs := make(map[string]bool)
	for i := 0; i < 10; i++ {
		go func() {
			for {
				m, err := s.Receive(ctx)
				if err != nil {
					// Permanent error; ctx cancelled or subscription closed is
					// expected once we've received all the messages.
					mu.Lock()
					n := len(receivedMsgs)
					mu.Unlock()
					if n != howManyToSend {
						t.Errorf("Worker's Receive failed before all messages were received (%d)", n)
					}
					return
				}
				mu.Lock()
				receivedMsgs[m.MessageID] = true
				mu.Unlock()
				wg.Done()
			}
		}()
	}

	// Send messages. Each message has a unique body used as a key to receivedMsgs.
	for i := 0; i < howManyToSend; i++ {
		key := fmt.Sprintf("message #%d", i)

		if err := dt.SendBatch(ctx, []*driver.DeliveryEvent{
			{
				MessageID: key,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Wait for the goroutines to receive all of the messages, then cancel the
	// ctx so they all exit.
	wg.Wait()
	defer cancel()

	// Check that all the messages were received.
	for i := 0; i < howManyToSend; i++ {
		key := fmt.Sprintf("message #%d", i)
		if !receivedMsgs[key] {
			t.Errorf("message %q was not received", key)
		}
	}
}

func TestCancelReceive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ds := NewDriverSub()
	s := NewDeliverySubscription(ds, nil)
	defer s.Shutdown(ctx)
	cancel()
	// Without cancellation, this Receive would hang.
	if _, err := s.Receive(ctx); err == nil {
		t.Error("got nil, want cancellation error")
	}
}

type blockingDriverSub struct {
	driver.DeliverySubscription
	inReceiveBatch chan struct{}
}

func (b blockingDriverSub) ReceiveBatch(ctx context.Context, maxMessages int) ([]*driver.DeliveryEvent, error) {
	b.inReceiveBatch <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingDriverSub) IsRetryable(error) bool { return false }
func (blockingDriverSub) Close() error           { return nil }

func TestCancelTwoReceives(t *testing.T) {
	// We want to create the following situation:
	// 1. Goroutine 1 calls Receive, obtains the lock (Subscription.mu),
	//    then releases the lock and calls driver.ReceiveBatch, which hangs.
	// 2. Goroutine 2 calls Receive.
	// 3. The context passed to the Goroutine 2 call is canceled.
	// We expect Goroutine 2's Receive to exit immediately. That won't
	// happen if Receive holds the lock during the call to ReceiveBatch.
	inReceiveBatch := make(chan struct{})
	s := NewDeliverySubscription(blockingDriverSub{inReceiveBatch: inReceiveBatch}, nil)
	defer s.Shutdown(context.Background())
	go func() {
		_, err := s.Receive(context.Background())
		// This should happen at the very end of the test, during Shutdown.
		if err != context.Canceled {
			t.Errorf("got %v, want context.Canceled", err)
		}
	}()
	<-inReceiveBatch
	// Give the Receive call time to block on the mutex before timing out.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	errc := make(chan error)
	go func() {
		_, err := s.Receive(ctx)
		errc <- err
	}()
	err := <-errc
	if err != context.DeadlineExceeded {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
}

var errRetry = errors.New("retry")

func isRetryable(err error) bool {
	return err == errRetry
}

const nRetryCalls = 2

func TestRetryReceive(t *testing.T) {
	ctx := context.Background()
	fs := &failSub{fail: true}
	sub := NewDeliverySubscription(fs, nil)
	defer sub.Shutdown(ctx)
	_, err := sub.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: got %v, want nil", err)
	}

	if got, want := fs.calls, nRetryCalls+1; got != want {
		t.Errorf("calls: got %d, want %d", got, want)
	}
}

// TestBatchSizeDecay verifies that the batch size decays when no messages are available.
// (see https://github.com/google/go-cloud/issues/2849).
func TestBatchSizeDecays(t *testing.T) {
	ctx := context.Background()
	fs := &failSub{}
	// Allow multiple handlers and cap max batch size to ensure we get concurrency.
	sub := NewDeliverySubscription(fs, &batcher.Options{MaxHandlers: 10, MaxBatchSize: 2})
	defer sub.Shutdown(ctx)

	// Records the last batch size.
	var mu sync.Mutex
	lastMaxMessages := 0
	sub.preReceiveBatchHook = func(maxMessages int) {
		mu.Lock()
		defer mu.Unlock()
		lastMaxMessages = maxMessages
	}

	// Do some receives to allow the number of batches to increase past 1.
	for n := 0; n < 100; n++ {
		_, err := sub.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: got %v, want nil", err)
		}
	}

	// Tell the failSub to start returning no messages.
	fs.mu.Lock()
	fs.empty = true
	fs.mu.Unlock()

	mu.Lock()
	highWaterMarkBatchSize := lastMaxMessages
	if lastMaxMessages <= 1 {
		t.Fatal("max messages wasn't greater than 1")
	}
	mu.Unlock()

	// Make a bunch of calls to Receive to drain any outstanding
	// messages, and wait some extra time during which we should
	// continue polling, and the batch size should decay.
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, err := sub.Receive(ctx)
		if err != nil {
			// Expected: no more messages, and timed out.
			break
		}
	}

	// Verify that the batch size decayed.
	mu.Lock()
	if lastMaxMessages >= highWaterMarkBatchSize {
		t.Fatalf("wanted batch size to decay; high water mark was %d, now %d", highWaterMarkBatchSize, lastMaxMessages)
	}
	mu.Unlock()
}

// TestRetryReceiveBatches verifies that batching and retries work without races
// (see https://github.com/google/go-cloud/issues/2676).
func TestRetryReceiveInBatchesDoesntRace(t *testing.T) {
	ctx := context.Background()
	fs := &failSub{}
	// Allow multiple handlers and cap max batch size to ensure we get concurrency.
	sub := NewDeliverySubscription(fs, &batcher.Options{MaxHandlers: 10, MaxBatchSize: 2})
	defer sub.Shutdown(ctx)

	// Do some receives to allow the number of batches to increase past 1.
	for n := 0; n < 100; n++ {
		_, err := sub.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: got %v, want nil", err)
		}
	}
	// Tell the failSub to start failing.
	fs.mu.Lock()
	fs.fail = true
	fs.mu.Unlock()

	// This call to Receive should result in nRetryCalls+1 calls to ReceiveBatch for
	// each batch. In the issue noted above, this would cause a race.
	for n := 0; n < 100; n++ {
		_, err := sub.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: got %v, want nil", err)
		}
	}
	// Don't try to verify the exact number of calls, as it is unpredictable
	// based on the timing of the batching.
}

// failSub helps test retries for ReceiveBatch.
//
// Once start=true, ReceiveBatch will fail nRetryCalls times before succeeding.
type failSub struct {
	driver.DeliverySubscription
	fail  bool
	empty bool
	calls int
	mu    sync.Mutex
}

func (t *failSub) ReceiveBatch(ctx context.Context, maxMessages int) ([]*driver.DeliveryEvent, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fail {
		t.calls++
		if t.calls <= nRetryCalls {
			return nil, errRetry
		}
	}
	if t.empty {
		t.calls++
		return nil, nil
	}
	return []*driver.DeliveryEvent{{MessageID: ""}}, nil
}

func (*failSub) IsRetryable(err error) bool { return isRetryable(err) }
func (*failSub) Close() error               { return nil }

// TODO(jba): add a test for retry of SendAcks.

var errDriver = errors.New("driver error")

type erroringSubscription struct {
	driver.DeliverySubscription
}

func (erroringSubscription) ReceiveBatch(context.Context, int) ([]*driver.DeliveryEvent, error) {
	return nil, errDriver
}

func (erroringSubscription) IsRetryable(err error) bool { return isRetryable(err) }
func (erroringSubscription) ErrorCode(error) codes.Code { return codes.AlreadyExists }
func (erroringSubscription) Close() error               { return errDriver }

// TestErrorsAreWrapped tests that all errors returned from the driver are
// wrapped exactly once by the portable type.
func TestErrorsAreWrapped(t *testing.T) {
	ctx := context.Background()

	verify := func(err error) {
		t.Helper()
		if err == nil {
			t.Errorf("got nil error, wanted non-nil")
			return
		}
		if e, ok := err.(*bcerrors.Error); !ok {
			t.Errorf("not wrapped: %v", err)
		} else if got := e.Unwrap(); got != errDriver {
			t.Errorf("got %v for wrapped error, not errDriver", got)
		}
		if s := err.Error(); !strings.HasPrefix(s, "email ") {
			t.Errorf("Error() for wrapped error doesn't start with 'pubsub': prefix: %s", s)
		}
	}

	sub := NewDeliverySubscription(erroringSubscription{}, nil)
	_, err := sub.Receive(ctx)
	verify(err)
	err = sub.Shutdown(ctx)
	verify(err)
}
