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
	"time"
)

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

	// TemplateName is the unique template name.
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

// ComposeTemplatedMessage composes a message from the given template.
func ComposeTemplatedMessage(composers ...ComposeTemplatedMsgFn) (*TemplatedMessage, error) {
	opts := messageOpts{
		isTemplate: true,
	}
	for _, composer := range composers {
		if err := composer.apply(&opts); err != nil {
			return nil, err
		}
	}

	tm := opts.toTemplateMessage()
	return tm, nil
}

// ComposeTemplatedMsgFn is a function that composes a message from the given template.
type ComposeTemplatedMsgFn interface {
	apply(*messageOpts) error
}

// Template composes a TemplatedMessage with given template name, version and data.
func Template(name, version string, data map[string]any) ComposeTemplatedMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if !msg.isTemplate {
			return errors.New("template cannot be set on a non template message")
		}

		msg.TemplateName = name
		msg.TemplateVersion = version
		msg.TemplateData = data
		return nil
	})
}

// TemplateName sets the template name.
func TemplateName(name string) ComposeTemplatedMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if !msg.isTemplate {
			return errors.New("template name cannot be set on a non template message")
		}
		msg.TemplateName = name
		return nil
	})
}

// TemplateVersion sets the template version.
func TemplateVersion(version string) ComposeTemplatedMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if !msg.isTemplate {
			return errors.New("template version cannot be set on a non template message")
		}
		msg.TemplateVersion = version
		return nil
	})
}

// TemplateData sets the template data.
func TemplateData(data map[string]any) ComposeTemplatedMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if !msg.isTemplate {
			return errors.New("template data cannot be set on a non template message")
		}
		msg.TemplateData = data
		return nil
	})
}
