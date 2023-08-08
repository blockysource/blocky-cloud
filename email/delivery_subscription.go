// Copyright 2023 The Blocky Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package email

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/googleapis/gax-go/v2"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"

	"github.com/blockysource/blocky-cloud/bcerrors"
	"github.com/blockysource/blocky-cloud/email/batcher"
	"github.com/blockysource/blocky-cloud/email/driver"
	"github.com/blockysource/go-pkg/retry"
)

// DeliveryEventType represents the type of an email delivery event.
type DeliveryEventType int

const (
	// DeliveryEventTypeDelivered indicates that the message was successfully delivered.
	// This event is sent when the message is accepted by the recipient's mail server.
	DeliveryEventTypeDelivered = DeliveryEventType(driver.DeliveryEventTypeDelivered)
	// DeliveryEventTypeBounce indicates that the message was rejected by the recipient's mail server.
	// This event is sent when the recipient's mail server permanently rejects the message.
	DeliveryEventTypeBounce = DeliveryEventType(driver.DeliveryEventTypeBounce)
	// DeliveryEventTypeDelayed indicates that the message was delayed.
	DeliveryEventTypeDelayed = DeliveryEventType(driver.DeliveryEventTypeDelayed)
	// DeliveryEventTypeRenderingFailure indicates that the message could not be rendered.
	// This event is sent when the message could not be rendered.
	DeliveryEventTypeRenderingFailure = DeliveryEventType(driver.DeliveryEventTypeRenderingFailure)
	// DeliveryEventTypeSend indicates that the message was send to the email service and is waiting for delivery.
	DeliveryEventTypeSend = DeliveryEventType(driver.DeliveryEventTypeSend)
	// DeliveryEventTypeFromAddressFailure indicates that the message could not be sent because the From address was invalid.
	// This event is sent when the From address is invalid, missing or does not have the required permissions (i.e. unverified).
	DeliveryEventTypeFromAddressFailure = DeliveryEventType(driver.DeliveryEventTypeFromAddressFailure)
)

func (d DeliveryEventType) String() string {
	switch d {
	case DeliveryEventTypeDelivered:
		return "delivered"
	case DeliveryEventTypeBounce:
		return "bounce"
	case DeliveryEventTypeDelayed:
		return "delayed"
	case DeliveryEventTypeRenderingFailure:
		return "rendering_failure"
	case DeliveryEventTypeSend:
		return "send"
	case DeliveryEventTypeFromAddressFailure:
		return "from_address_failure"
	default:
		return "unknown"
	}
}

// BounceType determines the type of the message bounce.
// It is used to determine whether the bounce is permanent or transient.
type BounceType int

// String returns the string representation of the bounce type.
func (bt BounceType) String() string {
	switch bt {
	case BounceTypeTransient:
		return "transient"
	case BounceTypePermanent:
		return "permanent"
	default:
		return "unknown"
	}
}

const (
	// BounceTypeTransient represents the bounce type of transient nature.
	BounceTypeTransient = BounceType(driver.BounceTypeTransient)
	// BounceTypePermanent represents the bounce type of permanent nature.
	BounceTypePermanent = BounceType(driver.BounceTypePermanent)
)

// DeliveryEvent represents an email delivery event.
type DeliveryEvent struct {
	// Type represents the type of the event.
	Type DeliveryEventType

	// Bounce contains the bounce information.
	// This field is only set if Type is DeliveryEventTypeBounce or DeliveryEventTypeTransientBounce.
	Bounce BounceRecord

	// Timestamp is the time when the event occurred.
	Timestamp time.Time

	// MessageID is the ID of the message associated with the event.
	MessageID string
}

type (
	// BounceRecord represents a bounce record.
	BounceRecord struct {
		// Recipients is a list of the bounced recipients.
		Recipients []BouncedRecipient
		// BounceType is the type of the bounce.
		BounceType BounceType
		// Timestamp is the time when the bounce occurred.
		Timestamp time.Time
	}
	// BouncedRecipient represents a bounced recipient.
	BouncedRecipient struct {
		// Email is the email address of the recipient.
		Email string
		// Status is the status of the bounce.
		Status string
		// DiagnosticCode is the diagnostic code of the bounce.
		DiagnosticCode string
	}
)

// DeliverySubscription represents a subscription to bounce events.
type DeliverySubscription struct {
	driver        driver.DeliverySubscription
	closed        bool
	cancel        func()
	backgroundCtx context.Context // for background SendAcks and ReceiveBatch calls

	recvBatchOpts *batcher.Options

	mu               sync.Mutex              // protects everything below
	q                []*driver.DeliveryEvent // local queue of messages downloaded from server
	err              error                   // permanent error
	waitc            chan struct{}           // for goroutines waiting on ReceiveBatch
	runningBatchSize float64                 // running number of messages to request via ReceiveBatch
	throughputStart  time.Time               // start time for throughput measurement
	throughputCount  int                     // number of msgs given out via Receive since throughputStart

	// Used in tests.
	preReceiveBatchHook func(maxMessages int)
}

// NewDeliverySubscription is intended for use by drivers only. Do not use in application code.
var NewDeliverySubscription = newDeliverySubscription

func newDeliverySubscription(d driver.DeliverySubscription, opts *batcher.Options) *DeliverySubscription {
	ctx, cancel := context.WithCancel(context.Background())
	mp := &DeliverySubscription{
		driver:           d,
		cancel:           cancel,
		backgroundCtx:    ctx,
		recvBatchOpts:    opts,
		runningBatchSize: initialBatchSize,
	}
	return mp
}

const (
	// The desired duration of a subscription's queue of messages (the messages pulled
	// and waiting in memory to be doled out to Receive callers). This is how long
	// it would take to drain the queue at the current processing rate.
	// The relationship to queue length (number of messages) is
	//
	//      lengthInMessages = desiredQueueDuration / averageProcessTimePerMessage
	//
	// In other words, if it takes 100ms to process a message on average, and we want
	// 2s worth of queued messages, then we need 2/.1 = 20 messages in the queue.
	//
	// If desiredQueueDuration is too small, then there won't be a large enough buffer
	// of messages to handle fluctuations in processing time, and the queue is likely
	// to become empty, reducing throughput. If desiredQueueDuration is too large, then
	// messages will wait in memory for a long time, possibly timing out (that is,
	// their ack deadline will be exceeded). Those messages could have been handled
	// by another process receiving from the same subscription.
	desiredQueueDuration = 2 * time.Second

	// Expected duration of calls to driver.ReceiveBatch, at some high percentile.
	// We'll try to fetch more messages when the current queue is predicted
	// to be used up in expectedReceiveBatchDuration.
	expectedReceiveBatchDuration = 1 * time.Second

	// s.runningBatchSize holds our current best guess for how many messages to
	// fetch in order to have a buffer of desiredQueueDuration. When we have
	// fewer than prefetchRatio * s.runningBatchSize messages left, that means
	// we expect to run out of messages in expectedReceiveBatchDuration, so we
	// should initiate another ReceiveBatch call.
	prefetchRatio = float64(expectedReceiveBatchDuration) / float64(desiredQueueDuration)

	// The initial # of messages to request via ReceiveBatch.
	initialBatchSize = 1

	// The factor by which old batch sizes decay when a new value is added to the
	// running value. The larger this number, the more weight will be given to the
	// newest value in preference to older ones.
	//
	// The delta based on a single value is capped by the constants below.
	decay = 0.5

	// The maximum growth factor in a single jump. Higher values mean that the
	// batch size can increase more aggressively. For example, 2.0 means that the
	// batch size will at most double from one ReceiveBatch call to the next.
	maxGrowthFactor = 2.0

	// Similarly, the maximum shrink factor. Lower values mean that the batch size
	// can shrink more aggressively. For example; 0.75 means that the batch size
	// will at most shrink to 75% of what it was before. Note that values less
	// than (1-decay) will have no effect because the running value can't change
	// by more than that.
	maxShrinkFactor = 0.75

	// The maximum batch size to request. Setting this too low doesn't allow
	// drivers to get lots of messages at once; setting it too small risks having
	// drivers spend a long time in ReceiveBatch trying to achieve it.
	maxBatchSize = 3000
)

// Receive receives and returns the next message from the subscription's queue,
// blocking and polling if none are available. It can be called concurrently
// from multiple goroutines.
//
// Receive retries retryable errors from the underlying driver forever.
// Therefore, if Receive returns an error, either:
// 1. It is a non-retryable error from the underlying driver, either from
//
//	an attempt to fetch more messages or from an attempt to ack messages.
//	Operator intervention may be required (e.g., invalid resource, quota
//	error, etc.). Receive will return the same error from then on, so the
//	application should log the error and either recreate the Subscription,
//	or exit.
//
// 2. The provided ctx is Done. Error() on the returned error will include both
//
//	the ctx error and the underlying driver error, and ErrorAs on it
//	can access the underlying driver error type if needed. Receive may
//	be called again with a fresh ctx.
//
// Callers can distinguish between the two by checking if the ctx they passed
// is Done, or via xerrors.Is(err, context.DeadlineExceeded or context.Canceled)
// on the returned error.
func (s *DeliverySubscription) Receive(ctx context.Context) (*DeliveryEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		// The lock is always held here, at the top of the loop.
		if s.err != nil {
			// The Subscription is in a permanent error state. Return the error.
			return nil, s.err // s.err wrapped when set
		}

		// Short circuit if ctx is Done.
		// Otherwise, we'll continue to return messages from the queue, and even
		// get new messages if driver.ReceiveBatch doesn't return an error when
		// ctx is done.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if s.waitc == nil && float64(len(s.q)) <= s.runningBatchSize*prefetchRatio {
			// We think we're going to run out of messages in expectedReceiveBatchDuration,
			// and there's no outstanding ReceiveBatch call, so initiate one in the
			// background.
			// Completion will be signalled to this goroutine, and to any other
			// waiting goroutines, by closing s.waitc.
			s.waitc = make(chan struct{})
			batchSize := s.updateBatchSize()
			go func() {
				if s.preReceiveBatchHook != nil {
					s.preReceiveBatchHook(batchSize)
				}
				msgs, err := s.getNextBatch(batchSize)
				s.mu.Lock()
				defer s.mu.Unlock()
				if err != nil {
					// Non-retryable error from ReceiveBatch -> permanent error.
					s.err = err
				} else if len(msgs) > 0 {
					s.q = append(s.q, msgs...)
				}
				close(s.waitc)
				s.waitc = nil
			}()
		}
		if len(s.q) > 0 {
			// At least one message is available. Return it.
			m := s.q[0]
			s.q = s.q[1:]
			s.throughputCount++

			// Convert driver.DeliveryEvent to DeliveryEvent.
			m2 := &DeliveryEvent{
				Type:      DeliveryEventType(m.Type),
				Timestamp: m.Timestamp,
				MessageID: m.MessageID,
			}
			if m2.Type == DeliveryEventTypeBounce {
				b := BounceRecord{
					Recipients: make([]BouncedRecipient, 0, len(m.Bounce.Recipients)),
					BounceType: BounceType(m.Bounce.BounceType),
					Timestamp:  m.Bounce.Timestamp,
				}
				for _, r := range m.Bounce.Recipients {
					b.Recipients = append(b.Recipients, BouncedRecipient{
						Email:          r.Email,
						Status:         r.Status,
						DiagnosticCode: r.DiagnosticCode,
					})
				}
				m2.Bounce = b
			}
			return m2, nil
		}
		// A call to ReceiveBatch must be in flight. Wait for it.
		waitc := s.waitc
		s.mu.Unlock()
		select {
		case <-waitc:
			s.mu.Lock()
			// Continue to top of loop.
		case <-ctx.Done():
			s.mu.Lock()
			return nil, ctx.Err()
		}
	}
}

// getNextBatch gets the next batch of messages from the server and returns it.
func (s *DeliverySubscription) getNextBatch(nMessages int) ([]*driver.DeliveryEvent, error) {
	var mu sync.Mutex
	var q []*driver.DeliveryEvent

	// Split nMessages into batches based on recvBatchOpts; we'll make a
	// separate ReceiveBatch call for each batch, and aggregate the results in
	// msgs.
	batches := batcher.Split(nMessages, s.recvBatchOpts)

	g, ctx := errgroup.WithContext(s.backgroundCtx)
	for _, maxMessagesInBatch := range batches {
		// Make a copy of the loop variable since it will be used by a goroutine.
		curMaxMessagesInBatch := maxMessagesInBatch
		g.Go(func() error {
			var msgs []*driver.DeliveryEvent
			err := retry.Call(ctx, gax.Backoff{}, s.driver.IsRetryable, func() error {
				var err error
				msgs, err = s.driver.ReceiveBatch(ctx, curMaxMessagesInBatch)
				return err
			})
			if err != nil {
				return wrapError(s.driver, err)
			}
			mu.Lock()
			defer mu.Unlock()
			q = append(q, msgs...)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return q, nil
}

// updateBatchSize updates the number of messages to request in ReceiveBatch
// based on the previous batch size and the rate of messages being pulled from
// the queue, measured using s.throughput*.
//
// It returns the number of messages to request in this ReceiveBatch call.
//
// s.mu must be held.
func (s *DeliverySubscription) updateBatchSize() int {
	// If we're always only doing one at a time, there's no point in this.
	if s.recvBatchOpts != nil && s.recvBatchOpts.MaxBatchSize == 1 && s.recvBatchOpts.MaxHandlers == 1 {
		return 1
	}
	now := time.Now()
	if s.throughputStart.IsZero() {
		// No throughput measurement; don't update s.runningBatchSize.
	} else {
		// Update s.runningBatchSize based on throughput since our last time here,
		// as measured by the ratio of the number of messages returned to elapsed
		// time.
		elapsed := now.Sub(s.throughputStart)
		if elapsed < 100*time.Millisecond {
			// Avoid divide-by-zero and huge numbers.
			elapsed = 100 * time.Millisecond
		}
		msgsPerSec := float64(s.throughputCount) / elapsed.Seconds()

		// The "ideal" batch size is how many messages we'd need in the queue to
		// support desiredQueueDuration at the msgsPerSec rate.
		idealBatchSize := desiredQueueDuration.Seconds() * msgsPerSec

		// Move s.runningBatchSize towards the ideal.
		// We first combine the previous value and the new value, with weighting
		// based on decay, and then cap the growth/shrinkage.
		newBatchSize := s.runningBatchSize*(1-decay) + idealBatchSize*decay
		if max := s.runningBatchSize * maxGrowthFactor; newBatchSize > max {
			s.runningBatchSize = max
		} else if min := s.runningBatchSize * maxShrinkFactor; newBatchSize < min {
			s.runningBatchSize = min
		} else {
			s.runningBatchSize = newBatchSize
		}
	}

	// Reset throughput measurement markers.
	s.throughputStart = now
	s.throughputCount = 0

	// Using Ceil guarantees at least one message.
	return int(math.Ceil(math.Min(s.runningBatchSize, maxBatchSize)))
}

var errSubscriptionShutdown = bcerrors.Newf(codes.FailedPrecondition, nil, "email: DeliverySubscription has been Shutdown")

// Shutdown flushes pending ack sends and disconnects the Subscription.
func (s *DeliverySubscription) Shutdown(ctx context.Context) (err error) {
	s.mu.Lock()
	if s.err == errSubscriptionShutdown {
		// Already Shutdown.
		defer s.mu.Unlock()
		return s.err
	}
	s.err = errSubscriptionShutdown
	s.mu.Unlock()
	select {
	case <-ctx.Done():
	default:
	}
	s.cancel()
	if err := s.driver.Close(); err != nil {
		return wrapError(s.driver, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return ctx.Err()
}

// As converts i to driver-specific types.
// See https://gocloud.dev/concepts/as/ for background information, the "As"
// examples in this package for examples, and the driver package
// documentation for the specific types supported for that driver.
func (s *DeliverySubscription) As(i interface{}) bool {
	return s.driver.As(i)
}

// ErrorAs converts err to driver-specific types.
// ErrorAs panics if i is nil or not a pointer.
// ErrorAs returns false if err == nil.
// See Topic.As for more details.
func (s *DeliverySubscription) ErrorAs(err error, i interface{}) bool {
	return bcerrors.ErrorAs(err, i, s.driver.ErrorAs)
}

type errorCoder interface {
	ErrorCode(error) codes.Code
}

func wrapError(ec errorCoder, err error) error {
	if err == nil {
		return nil
	}
	if bcerrors.DoNotWrap(err) {
		return err
	}
	return bcerrors.New(ec.ErrorCode(err), err, 2, "email")
}
