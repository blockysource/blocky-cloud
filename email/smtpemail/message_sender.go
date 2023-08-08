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

package smtpemail

import (
	"context"
	"errors"
	"fmt"
	"net/textproto"
	"strings"
	"time"

	"gocloud.dev/pubsub"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/blockysource/blocky-cloud/email/driver"
	mailingv1alpha "github.com/blockysource/go-genproto/blocky/mailing/v1alpha"
)

var _ driver.Sender = (*messageSender)(nil)

// messageSender is an implementation of the driver.Sender interface for the SMTP provider.
// It is responsible for parsing the input message and enqueueing it to the message queue.
// Any errors that occur during the parsing are returned to the caller.
// Any delivery errors could be returned by the message queue subcriber.
type messageSender struct {
	senders        *senderPool
	t              *pubsub.Topic
	autoCloseTopic bool
}

// newMessageSender creates a new messageSender.
func newMessageSender(pool *senderPool, t *pubsub.Topic, autoCloseTopic bool) *messageSender {
	return &messageSender{senders: pool, t: t, autoCloseTopic: autoCloseTopic}
}

// SendBatch implements driver.Sender.SendBatch.
// It parses the input message and enqueues it to the message queue.
// Any errors that occur during the parsing are returned to the caller.
func (m *messageSender) SendBatch(ctx context.Context, msgs []*driver.Message) error {
	for _, msg := range msgs {
		if err := prepareAndSend(ctx, msg, m.senders, m.t); err != nil {
			return err
		}
	}
	return nil
}

// ErrorAs implements driver.Sender.ErrorsAs.
func (m *messageSender) ErrorAs(err error, i interface{}) bool {
	e, ok := err.(*textproto.ProtocolError)
	if !ok {
		return false
	}

	ie, ok := i.(**textproto.ProtocolError)
	if !ok {
		return false
	}
	*ie = e

	return false
}

// ErrorCode implements driver.Sender.ErrorCode.
func (m *messageSender) ErrorCode(err error) codes.Code {
	return errorCode(err)
}

func errorCode(err error) codes.Code {
	var protoErr *textproto.Error
	if errors.As(err, &protoErr) {
		switch protoErr.Code {
		case 403:
			return codes.PermissionDenied
		case 421:
			return codes.ResourceExhausted
		case 450, 452:
			return codes.Unavailable
		case 451:
			if strings.Contains(protoErr.Msg, "Authentication") {
				return codes.Unauthenticated
			}
			return codes.FailedPrecondition
		case 455:
			return codes.Internal
		case 500, 501, 503:
			return codes.Internal
		case 502, 504:
			return codes.Unimplemented
		case 510:
			return codes.InvalidArgument
		case 512:
			return codes.NotFound
		case 530, 535, 538:
			return codes.Unauthenticated
		case 541:
			return codes.FailedPrecondition
		case 550:
			if strings.Contains(protoErr.Msg, "bad requests") {
				return codes.InvalidArgument
			}
			if strings.Contains(protoErr.Msg, "spam") {
				return codes.FailedPrecondition
			}
			return codes.InvalidArgument
		case 552:
			return codes.ResourceExhausted
		case 553:
			return codes.InvalidArgument
		}
	}

	return codes.Internal
}

// As implements driver.Sender.As.
func (m *messageSender) As(i interface{}) bool { return false }

// Close implements driver.Sender.Close.
func (m *messageSender) Close() error {
	// drain the pool and close the clients.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.senders.close(ctx); err != nil {
		return err
	}

	if m.autoCloseTopic && m.t != nil {
		return m.t.Shutdown(ctx)
	}
	return nil
}

func prepareAndSend(ctx context.Context, dmsg *driver.Message, senders *senderPool, t *pubsub.Topic) error {
	msg := getMessage()
	defer putMessage(msg)
	if err := msg.prepare(dmsg); err != nil {
		return err
	}

	if err := send(ctx, msg, senders, t); err != nil {
		return err
	}
	return nil
}

func send(ctx context.Context, msg *message, senders *senderPool, t *pubsub.Topic) error {
	sender, ok := senders.get()
	if !ok {
		// This should occur when the pool is closed.
		return errors.New("smtpemail: no senders available")
	}
	defer senders.put(sender)
	event, err := sender.sendMessage(ctx, msg)
	if err != nil {
		return err
	}

	if err = sendToTopic(ctx, event, t); err != nil {
		return err
	}

	return nil
}

func sendToTopic(ctx context.Context, event *driver.DeliveryEvent, t *pubsub.Topic) error {
	if t == nil {
		return nil
	}

	// Marshal the event to protobuf.

	msg := mailingv1alpha.DeliveryEvent{
		Type:      driverEventTypeToPbEventType(event.Type),
		MessageId: event.MessageID,
		Timestamp: timestamppb.New(event.Timestamp),
	}

	if event.Type == driver.DeliveryEventTypeBounce {
		msg.Bounce = &mailingv1alpha.BounceRecord{
			Type:      driverBounceTypeToPbBounceType(event.Bounce.BounceType),
			Timestamp: timestamppb.New(event.Bounce.Timestamp),
		}

		var recipients []*mailingv1alpha.BouncedRecipient
		for _, r := range event.Bounce.Recipients {
			recipients = append(recipients, &mailingv1alpha.BouncedRecipient{
				Email:  r.Email,
				Status: r.Status,
				Code:   r.DiagnosticCode,
			})
		}
		msg.Bounce.Recipients = recipients
	}

	data, err := proto.Marshal(&msg)
	if err != nil {
		return fmt.Errorf("smtpemail: failed to marshal delivery event: %w", err)
	}

	return t.Send(ctx, &pubsub.Message{Body: data})
}

func driverEventTypeToPbEventType(tp driver.DeliveryEventType) mailingv1alpha.DeliveryEventType {
	switch tp {
	case driver.DeliveryEventTypeDelivered:
		return mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_DELIVERED
	case driver.DeliveryEventTypeBounce:
		return mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_BOUNCE
	case driver.DeliveryEventTypeDelayed:
		return mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_DELAYED
	case driver.DeliveryEventTypeSend:
		return mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_SEND
	case driver.DeliveryEventTypeFromAddressFailure:
		return mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_FROM_FAILURE
	case driver.DeliveryEventTypeRenderingFailure:
		return mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_RENDERING_FAILURE
	default:
		return 0
	}
}

func driverBounceTypeToPbBounceType(tp driver.BounceType) mailingv1alpha.BounceType {
	switch tp {
	case driver.BounceTypePermanent:
		return mailingv1alpha.BounceType_BOUNCE_TYPE_PERMANENT
	case driver.BounceTypeTransient:
		return mailingv1alpha.BounceType_BOUNCE_TYPE_TRANSIENT
	default:
		return 0
	}
}
