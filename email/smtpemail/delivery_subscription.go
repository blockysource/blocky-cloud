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
	"time"

	"gocloud.dev/pubsub"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	"github.com/blockysource/blocky-cloud/email/driver"
	mailingv1alpha "github.com/blockysource/go-genproto/blocky/mailing/v1alpha"
)

var _ driver.DeliverySubscription = (*deliverySubscription)(nil)

// deliverySubscription represents a subscription to bounce events.
type deliverySubscription struct {
	sub       *pubsub.Subscription
	userOwner bool
}

// ReceiveBatch lets the provider receive a bounce event.
func (b *deliverySubscription) ReceiveBatch(ctx context.Context, maxMessages int) ([]*driver.DeliveryEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Millisecond*200)
	defer cancel()

	var out []*driver.DeliveryEvent
	for {
		select {
		case <-ctx.Done():
			return out, nil
		default:
			msg, err := b.sub.Receive(ctx)
			if err != nil {
				return nil, err
			}

			msg.Ack()

			var event mailingv1alpha.DeliveryEvent
			if err = proto.Unmarshal(msg.Body, &event); err != nil {
				return nil, err
			}

			de := driver.DeliveryEvent{
				Type:      protoDeliveryEventTypeToDriverType(event.Type),
				MessageID: event.GetMessageId(),
			}
			if event.GetTimestamp() != nil {
				de.Timestamp = event.GetTimestamp().AsTime()
			}
			if event.GetBounce() != nil {
				bounce := event.GetBounce()
				bounceRecord := driver.BounceRecord{
					BounceType: protoBounceTypeToDriverType(bounce.GetType()),
					Timestamp:  time.Time{},
				}

				if bounce.GetTimestamp() != nil {
					bounceRecord.Timestamp = bounce.GetTimestamp().AsTime()
				}

				for _, r := range bounce.GetRecipients() {
					bounceRecord.Recipients = append(bounceRecord.Recipients, driver.BouncedRecipient{
						Email:          r.GetEmail(),
						Status:         r.GetStatus(),
						DiagnosticCode: r.GetCode(),
					})
				}
				de.Bounce = bounceRecord
			}
			out = append(out, &de)
		}
	}
}

// As implements driver.DeliverySubscription.
func (b *deliverySubscription) As(i interface{}) bool {
	if v, ok := i.(**pubsub.Subscription); ok {
		*v = b.sub
		return true
	}
	return false
}

// ErrorAs implements driver.DeliverySubscription.
func (b *deliverySubscription) ErrorAs(err error, i interface{}) bool { return false }

// ErrorCode implements driver.DeliverySubscription.
func (b *deliverySubscription) ErrorCode(err error) codes.Code { return codes.Unknown }

// Close implements driver.DeliverySubscription.
func (b *deliverySubscription) Close() error {
	if b.userOwner {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	return b.sub.Shutdown(ctx)
}

// IsRetryable implements driver.DeliverySubscription.
func (b *deliverySubscription) IsRetryable(err error) bool {
	return false
}

func protoDeliveryEventTypeToDriverType(dt mailingv1alpha.DeliveryEventType) driver.DeliveryEventType {
	switch dt {
	case mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_DELIVERED:
		return driver.DeliveryEventTypeDelivered
	case mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_BOUNCE:
		return driver.DeliveryEventTypeBounce
	case mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_RENDERING_FAILURE:
		return driver.DeliveryEventTypeRenderingFailure
	case mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_SEND:
		return driver.DeliveryEventTypeSend
	case mailingv1alpha.DeliveryEventType_DELIVERY_EVENT_TYPE_DELAYED:
		return driver.DeliveryEventTypeDelayed
	default:
		return 0
	}
}

func protoBounceTypeToDriverType(bt mailingv1alpha.BounceType) driver.BounceType {
	switch bt {
	case mailingv1alpha.BounceType_BOUNCE_TYPE_PERMANENT:
		return driver.BounceTypePermanent
	case mailingv1alpha.BounceType_BOUNCE_TYPE_TRANSIENT:
		return driver.BounceTypeTransient
	default:
		return 0
	}
}
