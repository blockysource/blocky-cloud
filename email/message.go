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
	"errors"
	"sync"
	"time"
)

// Message is a message from the emails package.
type Message struct {
	mu sync.Mutex

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

	// ReplyTo is an email address that should be used to reply to the message.
	ReplyTo string

	// Date is a date when the email was sent.
	Date time.Time

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

// ComposeMsgFn is a message composer function.
type ComposeMsgFn interface {
	apply(*messageOpts) error
}

type composeFn func(msg *messageOpts) error

func (c composeFn) apply(msg *messageOpts) error {
	return c(msg)
}

// MessageID sets the loggable ID of the message.
func MessageID(id string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		msg.MessageID = id
		return nil
	})
}

func (m *Message) setMessageID(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.MessageID = id
}

// From sets the email sender.
func From(from string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if msg.From != "" {
			return errors.New("emails: from already set")
		}
		if from == "" {
			return errors.New("emails: from is empty")
		}

		msg.From = from
		return nil
	})
}

// ComposeMessage composes a message.
func ComposeMessage(composers ...ComposeMsgFn) (*Message, error) {
	opts := messageOpts{
		isTemplate: false,
	}
	for _, composer := range composers {
		if err := composer.apply(&opts); err != nil {
			return nil, err
		}
	}
	return opts.toMessage(), nil
}

// To sets the email recipients.
func To(to ...string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		msg.To = append(msg.To, to...)
		return nil
	})
}

// Cc sets the carbon copy email recipients.
func Cc(cc ...string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		msg.Cc = append(msg.Cc, cc...)
		return nil
	})
}

// Bcc sets the blind carbon copy email recipients.
func Bcc(bcc ...string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		msg.Bcc = append(msg.Bcc, bcc...)
		return nil
	})
}

// HTMLBody sets the HTML content of an email message.
func HTMLBody(content string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if msg.HtmlBody != "" {
			return errors.New("emails: html body already set")
		}
		msg.HtmlBody = content
		return nil
	})
}

// TextBody sets the text content of an email message.
func TextBody(content string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if msg.TextBody != "" {
			return errors.New("emails: text body already set")
		}
		msg.TextBody = content
		return nil
	})
}

// Subject sets the email subject.
func Subject(subject string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if msg.Subject != "" {
			return errors.New("emails: subject already set")
		}
		msg.Subject = subject
		if msg.Subject == "" {
			return errors.New("emails: subject is empty")
		}
		return nil
	})
}

// ReplyTo sets the email reply to addresses.
func ReplyTo(replyTo string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if msg.ReplyTo != "" {
			return errors.New("emails: reply to already set")
		}
		if replyTo == "" {
			return errors.New("emails: reply to is empty")
		}
		msg.ReplyTo = replyTo
		return nil
	})
}

// Date sets the email header Date.
func Date(date time.Time) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if !msg.Date.IsZero() {
			return errors.New("emails: date already set")
		}
		msg.Date = date
		return nil
	})
}

type messageOpts struct {
	MessageID       string
	From            string
	To              []string
	Cc              []string
	Bcc             []string
	HtmlBody        string
	TextBody        string
	Subject         string
	Attachments     []*Attachment
	ReplyTo         string
	Date            time.Time
	TemplateName    string
	TemplateVersion string
	TemplateData    map[string]any
	isTemplate      bool
}

func (o *messageOpts) toMessage() *Message {
	return &Message{
		MessageID:   o.MessageID,
		From:        o.From,
		To:          o.To,
		Cc:          o.Cc,
		Bcc:         o.Bcc,
		HtmlBody:    o.HtmlBody,
		TextBody:    o.TextBody,
		Subject:     o.Subject,
		Attachments: o.Attachments,
		ReplyTo:     o.ReplyTo,
		Date:        o.Date,
	}
}

func (o *messageOpts) toTemplateMessage() *TemplatedMessage {
	return &TemplatedMessage{
		MessageID:       o.MessageID,
		From:            o.From,
		To:              o.To,
		Cc:              o.Cc,
		Bcc:             o.Bcc,
		HtmlBody:        o.HtmlBody,
		TextBody:        o.TextBody,
		Subject:         o.Subject,
		Attachments:     o.Attachments,
		ReplyTo:         o.ReplyTo,
		Date:            o.Date,
		TemplateName:    o.TemplateName,
		TemplateVersion: o.TemplateVersion,
		TemplateData:    o.TemplateData,
	}
}
