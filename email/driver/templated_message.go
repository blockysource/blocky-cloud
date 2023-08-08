package driver

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
)

// TemplatedSender is an interface that represents a provider.
type TemplatedSender interface {
	// SendTemplatedMessageBatch lets the provider send the input message in a batch.
	SendTemplatedMessageBatch(ctx context.Context, msg []*TemplatedMessage) error

	// ErrorAs allows providers to expose provider-specific types for returned errors.
	ErrorAs(err error, i any) bool

	// ErrorCode should return a code that describes the error, which was returned by
	ErrorCode(error) codes.Code

	// As allows providers to expose provider-specific types.
	As(i any) bool

	// Close closes the provider and releases all resources.
	// This could be a noop for some providers.
	// Once closed is called, there will be no method calls to the Sender other than As, ErrorAs, and ErrorCode.
	Close() error
}

// TemplatedMessage is a message used for email templates.
type TemplatedMessage struct {
	// MessageID is the loggable ID of the message.
	MessageID string

	// From is the email sender.
	From string

	// To is a list of the email recipients.
	To []string

	// Cc is a list of the carbon copy email recipients.
	Cc []string

	// Bcc is a list of the blind carbon copy email recipients.
	Bcc []string

	// HtmlBody is the html body of an email message.
	HtmlBody string

	// TextBody is the text body of an email message.
	TextBody string

	// Subject is the email subject.
	Subject string

	// Attachments is a list of the email attachments.
	Attachments []*Attachment

	// Date the email date header.
	Date time.Time

	// ReplyTo is an email address that should be used to reply to the message.
	ReplyTo string

	// TemplateName is the ID of the template.
	TemplateName string

	// TemplateData is a map of the substitution data for the template.
	TemplateData map[string]any

	// TemplateVersion is the version of the template to be used.
	TemplateVersion string

	// BeforeSend is a callback used when sending a message. It will always be
	// set to nil for received messages.
	//
	// The callback will be called exactly once, before the message is sent.
	//
	// asFunc converts its argument to driver-specific types.
	// See https://gocloud.dev/concepts/as/ for background information.
	BeforeSend func(asFunc func(interface{}) bool) error

	// AfterSend is a callback used when sending a message. It will always be
	// set to nil for received messages.
	//
	// The callback will be called at most once, after the message is sent.
	// If Send returns an error, AfterSend will not be called.
	//
	// asFunc converts its argument to driver-specific types.
	// See https://gocloud.dev/concepts/as/ for background information.
	AfterSend func(asFunc func(interface{}) bool) error
}
