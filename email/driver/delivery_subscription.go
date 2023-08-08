package driver

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
)

// DeliveryEventType represents the type of a delivery event.
type DeliveryEventType int

const (
	// DeliveryEventTypeDelivered indicates that the message was successfully delivered.
	// This event is sent when the message is accepted by the recipient's mail server.
	DeliveryEventTypeDelivered DeliveryEventType = 0
	// DeliveryEventTypeBounce indicates that the message was rejected by the recipient's mail server.
	// This event is sent when the recipient's mail server permanently rejects the message.
	DeliveryEventTypeBounce DeliveryEventType = 1
	// DeliveryEventTypeDelayed indicates that the message was delayed.
	DeliveryEventTypeDelayed DeliveryEventType = 2
	// DeliveryEventTypeRenderingFailure indicates that the message could not be rendered.
	// This event is sent when the message could not be rendered.
	DeliveryEventTypeRenderingFailure DeliveryEventType = 3
	// DeliveryEventTypeSend indicates that the message was send to the email service and is waiting for delivery.
	DeliveryEventTypeSend DeliveryEventType = 4
	// DeliveryEventTypeFromAddressFailure indicates that the message could not be sent because the From address was invalid.
	// This event is sent when the From address is invalid, missing or does not have the required permissions (i.e. unverified).
	DeliveryEventTypeFromAddressFailure DeliveryEventType = 5
)

// DeliveryEvent represents a delivery event.
type DeliveryEvent struct {
	// Type describes the type of the event.
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
// It is used to receive bounce events.
type DeliverySubscription interface {
	// ReceiveBounceBatch lets the provider receive a bounce event.
	// The method blocks until a bounce event is received or the context is canceled.
	ReceiveBatch(ctx context.Context, maxMessages int) ([]*DeliveryEvent, error)

	// As allows providers to expose provider-specific types.
	As(i interface{}) bool

	// ErrorAs allows providers to expose provider-specific types for returned errors.
	ErrorAs(err error, i interface{}) bool

	// ErrorCode should return a code that describes the error, which was returned by
	ErrorCode(error) codes.Code

	// IsRetryable should return true if the error is retryable.
	IsRetryable(err error) bool

	// Close closes the subscription.
	Close() error
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
	_ BounceType = iota
	// BounceTypeTransient indicates a transient bounce.
	BounceTypeTransient
	// BounceTypePermanent indicates a permanent bounce.
	BounceTypePermanent
)

// BounceNotification is a bounce notification.
type BounceNotification struct {
	// MessageID is the loggable ID of the message.
	MessageID string

	// Details is the bounce details.
	Details string

	// Type is the bounce type.
	Type BounceType
}
