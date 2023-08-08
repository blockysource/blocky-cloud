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
	"fmt"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"gocloud.dev/pubsub"

	"github.com/blockysource/blocky-cloud/email/batcher"
	"github.com/blockysource/blocky-cloud/email/emailtemplate"
	"github.com/blockysource/blocky/open-source/libs/blocky-cloud/email"
)

func init() {
	email.DefaultURLMux().RegisterSender(Scheme, new(lazyURLOpener))
	email.DefaultURLMux().RegisterTemplatedSender(Scheme, new(URLOpener))
}

var defaultBatcherOptions = batcher.Options{
	MaxHandlers:  1,
	MinBatchSize: 1,
	MaxBatchSize: 1,
}

type lazyURLOpener struct {
	init   sync.Once
	opener *URLOpener
	err    error
}

func (o *lazyURLOpener) openDialer() error {
	o.init.Do(func() {
		smtpURL := os.Getenv("BLOCKY_SMTP_URL")
		if smtpURL == "" {
			o.err = fmt.Errorf("smtpemail: BLOCKY_SMTP_URL environment variable is not set")
			return
		}

		d, err := ParseDialerURL(smtpURL)
		if err != nil {
			o.err = err
			return
		}

		o.opener = &URLOpener{
			Dialer:    d,
			Templates: defaultTemplates,
			SenderOptions: SenderOptions{
				Encoding:     QuotedPrintable,
				MaxBatchSize: 1,
				MaxHandlers:  1,
				Charset:      "utf-8",
			},
		}
	})
	return o.err
}

// OpenSenderURL opens the mail provider at the URLs path.
func (o *lazyURLOpener) OpenSenderURL(ctx context.Context, u *url.URL) (*email.Sender, error) {
	if err := o.openDialer(); err != nil {
		return nil, err
	}

	return o.opener.OpenSenderURL(ctx, u)
}

// OpenTemplatedSenderURL opens the mail provider at the URLs path.
func (o *lazyURLOpener) OpenTemplatedSenderURL(ctx context.Context, u *url.URL) (*email.TemplatedSender, error) {
	if err := o.openDialer(); err != nil {
		return nil, err
	}

	return o.opener.OpenTemplatedSenderURL(ctx, u)
}

func smtpAuth(username, password, host, auth string) (smtp.Auth, error) {
	switch strings.ToLower(auth) {
	case "plain":
		return smtp.PlainAuth("", username, password, host), nil
	case "login":
		return &loginAuth{username: username, password: password}, nil
	case "md5":
		return smtp.CRAMMD5Auth(username, password), nil
	default:
		return nil, fmt.Errorf("smtpemail: invalid authentication mechanism: %s", auth)
	}
}

// Scheme is the URL scheme smtpmailprovider registers its URLOpener under on mailprovider.DefaultMux.
const Scheme = "smtp"

// URLOpener opens smtp URLs like
// "smtp://?encoding=quoted-printable&maxbatchsize=10&auth=md5"
//
// The URL Host+Port are used as the SMTP server address.
// The following query parameters are supported:
//
//   - charset: the charset of the message body. By default, "utf-8" is used.
//   - encoding: the encoding of the message body. By default, "quoted-printable" is used.
//     The following values are supported: "quoted-printable", "base64", "binary".
//   - maxbatchsize: the maximum number of messages sent in a single batch.
//     By default, 1 is used.
//   - maxhandlers: the maximum number of parallel handlers used to send
//     messages. By default, 1 is used.
//   - deliverytopic: optional parameter to specify the topic to which the
//     delivery event is published. If not specified, no delivery event is
//	   published. The topic query must follow the format described in the
//     gocloud.dev/pubsub package documentation, and load suitable driver for it.
//     The topic needs to be query parameter encoded.
type URLOpener struct {
	// Dialer is the dialer used to connect to the SMTP server.
	Dialer *Dialer
	// SenderOptions contains options for the Sender.
	SenderOptions SenderOptions
	// Templates is a set of parsed templates used to render the templated messages.
	Templates *emailtemplate.ParsedTemplates
	// TopicOpener is used to open the topic for the delivery event.
	// If nil, the default pubsub.URLMux is used.
	TopicOpener *pubsub.URLMux
}

// Encoding represents a MIME encoding scheme like quoted-printable or base64.
type Encoding string

const (
	// QuotedPrintable represents the quoted-printable encoding as defined in
	// RFC 2045.
	QuotedPrintable Encoding = "quoted-printable"
	// Base64 represents the base64 encoding as defined in RFC 2045.
	Base64 Encoding = "base64"
	// Unencoded can be used to avoid encoding the body of an email. The headers
	// will still be encoded using quoted-printable encoding.
	Unencoded Encoding = "8bit"
)

// FromString parses an Encoding from a string.
func (e *Encoding) FromString(in string) error {
	switch in {
	case "quoted-printable", "qp", "quoted", "quotedprintable":
		*e = QuotedPrintable
	case "base64", "b64":
		*e = Base64
	case "unencoded", "8bit", "8bits", "8-bit", "8-bits":
		*e = Unencoded
	}
	return nil
}

// SenderOptions contains options for the Sender.
type SenderOptions struct {
	// Encoding determines the Content-Transfer-Encoding of the message body.
	// By default, "quoted-printable" is used.
	Encoding Encoding
	// MaxBatchSize is the maximum number of messages sent in a single batch.
	// By default, 1 is used.
	MaxBatchSize int
	// MaxHandlers is the maximum number of parallel handlers used to send
	// messages. By default, 1 is used.
	MaxHandlers int
	// Charset is the charset of the message body.
	// By default, "utf-8" is used.
	Charset string
}

// OpenSenderURL opens the mail provider at the URLs path.
func (o *URLOpener) OpenSenderURL(ctx context.Context, u *url.URL) (*email.Sender, error) {
	if err := o.Dialer.Validate(); err != nil {
		return nil, err
	}

	opts := o.SenderOptions
	var topicURL string
	for k, v := range u.Query() {
		switch strings.ToLower(k) {
		case "encoding":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid encoding: %s", v)
			}
			if err := opts.Encoding.FromString(v[0]); err != nil {
				return nil, fmt.Errorf("smtpemail: invalid bodyencoding: %s", v)
			}
		case "maxbatchsize":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid maxbatchsize: %s", v)
			}
			maxbatchsize, err := strconv.ParseInt(v[0], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("smtpemail: invalid maxbatchsize: %s", v)
			}
			opts.MaxBatchSize = int(maxbatchsize)
		case "maxhandlers":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid maxhandlers: %s", v)
			}
			maxhandlers, err := strconv.ParseInt(v[0], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("smtpemail: invalid maxhandlers: %s", v)
			}
			opts.MaxHandlers = int(maxhandlers)
		case "charset":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid charset: %s", v)
			}
			opts.Charset = v[0]
		case "deliverytopic":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid deliverytopic: %s", v)
			}
			topicURL = v[0]
		default:
			return nil, fmt.Errorf("smtpemail: invalid query parameter: %s", k)
		}
	}

	var deliveryTopic *pubsub.Topic
	if topicURL != "" {
		mux := o.TopicOpener
		if mux == nil {
			mux = pubsub.DefaultURLMux()
		}
		var err error
		deliveryTopic, err = mux.OpenTopic(ctx, topicURL)
		if err != nil {
			return nil, fmt.Errorf("smtpemail: failed to open delivery topic: %v", err)
		}
	}

	return OpenSender(ctx, o.Dialer, opts, deliveryTopic)
}

// OpenSender opens the Sender based on the provided Dialer.
// The deliveryTopic is optional and can be used to publish delivery events to the provided topic.
// If the deliveryTopic is nil, no delivery events will be published.
func OpenSender(ctx context.Context, d *Dialer, opts SenderOptions, deliveryTopic *pubsub.Topic) (*email.Sender, error) {
	return openSender(ctx, d, opts, deliveryTopic, false)
}

// OpenTemplatedSenderURL opens the mail provider at the URLs path.
func (o *URLOpener) OpenTemplatedSenderURL(ctx context.Context, u *url.URL) (*email.TemplatedSender, error) {
	if err := o.Dialer.Validate(); err != nil {
		return nil, err
	}

	opts := o.SenderOptions
	var topicURL string
	for k, v := range u.Query() {
		switch strings.ToLower(k) {
		case "encoding":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid encoding: %s", v)
			}
			if err := opts.Encoding.FromString(v[0]); err != nil {
				return nil, fmt.Errorf("smtpemail: invalid bodyencoding: %s", v)
			}
		case "maxbatchsize":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid maxbatchsize: %s", v)
			}
			maxbatchsize, err := strconv.ParseInt(v[0], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("smtpemail: invalid maxbatchsize: %s", v)
			}
			opts.MaxBatchSize = int(maxbatchsize)
		case "charset":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid charset: %s", v)
			}
			opts.Charset = v[0]
		case "deliverytopic":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid deliverytopic: %s", v)
			}
			topicURL = v[0]
		default:
			return nil, fmt.Errorf("smtpemail: invalid query parameter: %s", k)
		}
	}
	var deliveryTopic *pubsub.Topic
	if topicURL != "" {
		mux := o.TopicOpener
		if mux == nil {
			mux = pubsub.DefaultURLMux()
		}
		var err error
		deliveryTopic, err = mux.OpenTopic(ctx, topicURL)
		if err != nil {
			return nil, fmt.Errorf("smtpemail: failed to open delivery topic: %v", err)
		}
	}

	return OpenTemplatedSender(ctx, o.Dialer, opts, o.Templates, deliveryTopic)
}

func openSender(ctx context.Context, d *Dialer, opts SenderOptions, deliveryTopic *pubsub.Topic, autoCloseTopic bool) (*email.Sender, error) {
	if opts.MaxHandlers <= 0 {
		opts.MaxHandlers = 1
	}
	if opts.MaxBatchSize <= 0 {
		opts.MaxBatchSize = 1
	}
	sp, err := newSenderPool(ctx, d, opts)
	if err != nil {
		return nil, err
	}

	ss := newMessageSender(sp, deliveryTopic, autoCloseTopic)

	bo := defaultBatcherOptions
	if opts.MaxBatchSize > 0 {
		bo.MaxBatchSize = opts.MaxBatchSize
	}

	return email.NewSender(ss, &bo), nil
}

func openSmtpSender(ctx context.Context, d *Dialer, opts SenderOptions) (*smtpSender, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	c, err := d.dial(ctx)
	if err != nil {
		return nil, err
	}
	ss := &smtpSender{c: c, d: d, opts: opts}

	return ss, nil
}
