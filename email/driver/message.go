package driver

import (
	"context"
	"io"
	"time"

	"google.golang.org/grpc/codes"
)

// Sender is an interface that represents a provider.
type Sender interface {
	// SendBatch lets the provider send the input message in a batch.
	SendBatch(ctx context.Context, msg []*Message) error

	// ErrorAs allows providers to expose provider-specific types for returned errors.
	ErrorAs(err error, i interface{}) bool

	// ErrorCode should return a code that describes the error, which was returned by
	ErrorCode(error) codes.Code

	// As allows providers to expose provider-specific types.
	As(i interface{}) bool

	// Close closes the provider and releases all resources.
	// This could be a noop for some providers.
	// Once closed is called, there will be no method calls to the Sender other than As, ErrorAs, and ErrorCode.
	Close() error
}

// Message is a message.
type Message struct {
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

	// BeforeSend is a callback used when sending a message. It will always be
	// set to nil for received messages.
	//
	// The callback will be called exactly once, before the message is sent.
	//
	// asFunc converts its argument to driver-specific types.
	// See https://gocloud.dev/concepts/as/ for background information.
	BeforeSend func(asFunc func(any) bool) error

	// AfterSend is a callback used when sending a message. It will always be
	// set to nil for received messages.
	//
	// The callback will be called at most once, after the message is sent.
	// If Send returns an error, AfterSend will not be called.
	//
	// asFunc converts its argument to driver-specific types.
	// See https://gocloud.dev/concepts/as/ for background information.
	AfterSend func(asFunc func(any) bool) error
}

// Attachment is an interface used for email attachments.
type Attachment struct {
	// ContentID is the attachment content identifier.
	// This value is optional, in case it is not set, a <filename> will be used.
	ContentID string
	// Filename is the attachment filename.
	File AttachmentFile
	// Filename is the attachment filename.
	Filename string
	// ContentType is the attachment content type.
	ContentType string
	// Inline determines if the attachment should be displayed inline.
	Inline bool
}

// AttachmentFile is an interface used for email attachment files.
type AttachmentFile interface {
	io.Reader
	ContentType() string
	ModTime() time.Time
	Size() int64
}

// Read reads up to len(p) bytes into p.
func (a *Attachment) Read(p []byte) (n int, err error) {
	return a.File.Read(p)
}

// WriteTo writes the attachment to w.
func (a *Attachment) WriteTo(w io.Writer) (n int64, err error) {
	return io.Copy(w, a.File)
}

// Close closes the attachment.
func (a *Attachment) Close() error {
	if f, ok := a.File.(io.Closer); ok {
		return f.Close()
	}
	return nil
}
