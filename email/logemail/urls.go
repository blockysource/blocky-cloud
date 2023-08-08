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

package logemail

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"gocloud.dev/gcerrors"
	"golang.org/x/exp/slog"
	"google.golang.org/grpc/codes"

	"github.com/blockysource/blocky-cloud/email"
	"github.com/blockysource/blocky-cloud/email/driver"
)

func init() {
	o := new(defaultOpener)
	email.DefaultURLMux().RegisterSender(Scheme, o)
	email.DefaultURLMux().RegisterTemplatedSender(Scheme, o)
}

type defaultOpener struct {
	init   sync.Once
	opener URLOpener
}

func (o *defaultOpener) initLogger() {
	o.init.Do(func() {
		l := slog.New(slog.NewJSONHandler(os.Stdout))
		o.opener.Logger = l
	})
}

// OpenSenderURL opens email.Sender based on provider url.
func (o *defaultOpener) OpenSenderURL(ctx context.Context, u *url.URL) (*email.Sender, error) {
	o.initLogger()
	return o.opener.OpenSenderURL(ctx, u)
}

// OpenTemplatedSenderURL opens email.TemplatedSender based on provider url.
func (o *defaultOpener) OpenTemplatedSenderURL(ctx context.Context, u *url.URL) (*email.TemplatedSender, error) {
	o.initLogger()
	return o.opener.OpenTemplatedSenderURL(ctx, u)
}

// Scheme is the URL scheme logemail registers its URLOpener under on emails.DefaultMux.
const Scheme = "log"

// SenderOptions are options for constructing a *email.Sender and *email.TemplatedSender
// for the logemail provider.
type SenderOptions struct {
	// LogLevel is the level at which to log messages.
	LogLevel slog.Level
	// LogBody controls whether the body of the message is logged.
	LogBody bool
}

// URLOpener is a logging email.SenderURLOpener and email.TemplatedSenderURLOpener.
// This should not be used in production.
// It is intended for use in tests and as an example or starting point for implementing
// email.SenderURLOpener and email.TemplatedSenderURLOpener.
type URLOpener struct {
	// Logger is the logger used to log messages.
	// If nil, slog.Default is used.
	Logger *slog.Logger
}

// OpenSenderURL opens email.Sender based on provider url.
// Implements email.SenderURLOpener.
// The url is of the form "log://?level=debug&logbody=true".
func (o *URLOpener) OpenSenderURL(ctx context.Context, u *url.URL) (*email.Sender, error) {
	opts, err := o.parseSenderOptions(ctx, u)
	if err != nil {
		return nil, err
	}
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}

	return email.NewSender(&logSender{logFn: senderLogFn(log, opts)}, nil), nil
}

// OpenSender opens email.Sender based on provider url.
func OpenSender(logger *slog.Logger, opts SenderOptions) (*email.Sender, error) {
	return email.NewSender(&logSender{logFn: senderLogFn(logger, opts)}, nil), nil
}

// OpenSenderLogFn opens log based  email.Sender with custom log function.
func OpenSenderLogFn(logFn func(ctx context.Context, msg *driver.Message)) (*email.Sender, error) {
	return email.NewSender(&logSender{logFn: logFn}, nil), nil
}

func senderLogFn(logger *slog.Logger, opts SenderOptions) func(ctx context.Context, msg *driver.Message) {
	return func(ctx context.Context, msg *driver.Message) {
		args := []slog.Attr{
			{Key: "messageID", Value: slog.StringValue(msg.MessageID)},
			{Key: "from", Value: slog.AnyValue(msg.From)},
			{Key: "to", Value: slog.AnyValue(msg.To)},
			{Key: "cc", Value: slog.AnyValue(msg.Cc)},
			{Key: "bcc", Value: slog.AnyValue(msg.Bcc)},
			{Key: "subject", Value: slog.StringValue(msg.Subject)},
			{Key: "replyTo", Value: slog.AnyValue(msg.ReplyTo)},
			{Key: "date", Value: slog.AnyValue(msg.Date)},
		}
		if opts.LogBody {
			args = append(args,
				slog.String("htmlBody", msg.HtmlBody),
				slog.String("textBody", msg.TextBody))
		}

		if len(msg.Attachments) > 0 {
			for i, a := range msg.Attachments {
				args = append(args,
					slog.Any(fmt.Sprintf("attachment#%d", i), attachment{
						contentID:   a.ContentID,
						filename:    a.Filename,
						contentType: a.ContentType,
						inline:      a.Inline,
					}),
				)
			}
		}
		logger.Log(ctx, opts.LogLevel, "sending message", "message: ", slog.GroupValue(args...))
	}
}

// OpenTemplatedSenderURL opens email.TemplatedSender based on provider url.
// Implements email.TemplatedSenderURLOpener.
// The url is of the form "log://?level=debug&logbody=true".
func (o *URLOpener) OpenTemplatedSenderURL(ctx context.Context, u *url.URL) (*email.TemplatedSender, error) {
	opts, err := o.parseSenderOptions(ctx, u)
	if err != nil {
		return nil, err
	}

	log := o.Logger
	if log == nil {
		log = slog.Default()
	}

	return email.NewTemplatedSender(&logTemplateSender{logFn: templateSenderLogFn(log, opts)}, nil), nil
}

// OpenTemplatedSender opens email.TemplatedSender based on provider url.
func OpenTemplatedSender(logger *slog.Logger, opts SenderOptions) (*email.TemplatedSender, error) {
	return email.NewTemplatedSender(&logTemplateSender{logFn: templateSenderLogFn(logger, opts)}, nil), nil
}

// OpenTemplatedSenderLogFn opens log based email.TemplatedSender with custom log function.
func OpenTemplatedSenderLogFn(logFn func(ctx context.Context, msg *driver.TemplatedMessage)) (*email.TemplatedSender, error) {
	return email.NewTemplatedSender(&logTemplateSender{logFn: logFn}, nil), nil
}

func templateSenderLogFn(logger *slog.Logger, opts SenderOptions) func(ctx context.Context, msg *driver.TemplatedMessage) {
	return func(ctx context.Context, msg *driver.TemplatedMessage) {
		args := []slog.Attr{
			{Key: "messageID", Value: slog.StringValue(msg.MessageID)},
			{Key: "from", Value: slog.StringValue(msg.From)},
			{Key: "to", Value: slog.AnyValue(msg.To)},
			{Key: "cc", Value: slog.AnyValue(msg.Cc)},
			{Key: "bcc", Value: slog.AnyValue(msg.Bcc)},
			{Key: "subject", Value: slog.StringValue(msg.Subject)},
			{Key: "replyTo", Value: slog.AnyValue(msg.ReplyTo)},
			{Key: "date", Value: slog.AnyValue(msg.Date)},
			slog.Any("template", templateLogger{
				name:    msg.TemplateName,
				version: msg.TemplateVersion,
				data:    msg.TemplateData,
			}),
		}
		if opts.LogBody {
			args = append(args,
				slog.String("htmlBody", msg.HtmlBody),
				slog.String("textBody", msg.TextBody),
			)
		}

		if len(msg.Attachments) > 0 {
			for i, a := range msg.Attachments {
				args = append(args,
					slog.Any(fmt.Sprintf("attachment#%d", i), attachment{
						contentID:   a.ContentID,
						filename:    a.Filename,
						contentType: a.ContentType,
						inline:      a.Inline,
					}),
				)
			}
		}
		logger.Log(ctx, opts.LogLevel, "message", slog.GroupValue(args...))
	}
}

type attachment struct {
	contentID   string
	filename    string
	contentType string
	inline      bool
}

func (a attachment) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("contentID", a.contentID),
		slog.String("filename", a.filename),
		slog.String("contentType", a.contentType),
		slog.Bool("inline", a.inline),
	)
}

// logSender is an implementation of the email.Sender interface that logs all messages to the logMessageFn.
type logSender struct {
	logFn func(ctx context.Context, msg *driver.Message)
}

// SendBatch implements driver.Sender.SendBatch.
// It logs all messages to the logMessageFn.
func (l *logSender) SendBatch(ctx context.Context, msgs []*driver.Message) error {
	for _, msg := range msgs {
		l.logFn(ctx, msg)
	}
	return nil
}

func (l *logSender) Close() error { return nil }

func (l *logSender) ErrorAs(err error, i interface{}) bool {
	return false
}

func (l *logSender) ErrorCode(err error) codes.Code { return codes.Unknown }

func (l *logSender) As(i interface{}) bool {
	impl, ok := i.(*func(ctx context.Context, msg *driver.Message))
	if !ok {
		return false
	}
	*impl = l.logFn
	return true
}

func (o *URLOpener) parseSenderOptions(ctx context.Context, u *url.URL) (SenderOptions, error) {
	var opts SenderOptions
	for k, v := range u.Query() {
		switch strings.ToLower(k) {
		case "level":
			if len(v) != 1 {
				return SenderOptions{}, fmt.Errorf("logemail: invalid level: %s", v)
			}
			switch k {
			case "debug":
				opts.LogLevel = slog.LevelDebug
			case "info":
				opts.LogLevel = slog.LevelInfo
			case "warn":
				opts.LogLevel = slog.LevelWarn
			case "error":
				opts.LogLevel = slog.LevelError
			case "":
			default:
				li, err := strconv.Atoi(v[0])
				if err != nil {
					return SenderOptions{}, fmt.Errorf("logemail: invalid level %q: %v", v[0], err)
				}
				opts.LogLevel = slog.Level(li)
			}
		case "logbody":
			if len(v) != 1 {
				return SenderOptions{}, fmt.Errorf("logemail: invalid 'logbody': %s", v)
			}

			if v[0] == "" {
				opts.LogBody = true
			} else {
				lb, err := strconv.ParseBool(v[0])
				if err != nil {
					return SenderOptions{}, fmt.Errorf("logemail: invalid 'logbody': %s", v)
				}
				opts.LogBody = lb
			}
		default:
			return SenderOptions{}, fmt.Errorf("logemail: invalid option %q", k)
		}
	}
	return opts, nil
}

type logTemplateSender struct {
	logFn func(ctx context.Context, msg *driver.TemplatedMessage)
}

func (l *logTemplateSender) SendTemplatedMessageBatch(ctx context.Context, msgs []*driver.TemplatedMessage) error {
	for _, msg := range msgs {
		l.logFn(ctx, msg)
	}
	return nil
}

func (l *logTemplateSender) Close() error { return nil }

func (l *logTemplateSender) ErrorAs(err error, i interface{}) bool {
	return false
}

func (l *logTemplateSender) ErrorCode(err error) codes.Code { return gcerrors.Unknown }

func (l *logTemplateSender) As(i interface{}) bool {
	impl, ok := i.(*func(ctx context.Context, msg *driver.TemplatedMessage))
	if !ok {
		return false
	}
	*impl = l.logFn
	return true
}

type templateLogger struct {
	name    string
	version string
	data    map[string]any
}

func (t templateLogger) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("name", t.name),
		slog.String("version", t.version),
		slog.Any("data", t.data),
	)
}
