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

	"gocloud.dev/pubsub"
	"google.golang.org/grpc/codes"

	"github.com/blockysource/blocky-cloud/email"
	"github.com/blockysource/blocky-cloud/email/driver"
	"github.com/blockysource/blocky-cloud/email/emailtemplate"
)

var (
	defaultTemplates = new(emailtemplate.ParsedTemplates)
)

// AddTemplate adds a template to the default templates.
func AddTemplate(t *emailtemplate.Template) error {
	return defaultTemplates.Add(t)
}

// AddTemplateVersion adds a template version to the default templates.
func AddTemplateVersion(tempName string, t *emailtemplate.TemplateVersion) error {
	return defaultTemplates.AddVersion(tempName, t)
}

// UpsertTemplateVersion adds or updates a template version to the default templates.
func UpsertTemplateVersion(tempName string, t *emailtemplate.TemplateVersion) error {
	return defaultTemplates.UpsertVersion(tempName, t)
}

var _ driver.TemplatedSender = (*templatedSender)(nil)

type templatedSender struct {
	s         *senderPool
	templates *emailtemplate.ParsedTemplates
	topic     *pubsub.Topic
}

// OpenTemplatedSender opens the TemplatedSender based on the provided Dialer.
func OpenTemplatedSender(ctx context.Context, d *Dialer, opts SenderOptions, t *emailtemplate.ParsedTemplates, delivery *pubsub.Topic) (*email.TemplatedSender, error) {
	if opts.MaxBatchSize <= 0 {
		opts.MaxBatchSize = 1
	}
	if opts.MaxHandlers <= 0 {
		opts.MaxHandlers = 1
	}
	ss, err := openTemplatedSender(ctx, d, opts, t, delivery)
	if err != nil {
		return nil, err
	}

	bo := defaultBatcherOptions
	if opts.MaxBatchSize > 0 {
		bo.MaxBatchSize = opts.MaxBatchSize
	}
	ss.templates = t

	return email.NewTemplatedSender(ss, &bo), nil
}

func openTemplatedSender(ctx context.Context, d *Dialer, opts SenderOptions, t *emailtemplate.ParsedTemplates, dt *pubsub.Topic) (*templatedSender, error) {
	if t == nil {
		return nil, errors.New("smtpemail: input templates are nil")
	}
	s, err := newSenderPool(ctx, d, opts)
	if err != nil {
		return nil, err
	}
	return &templatedSender{s: s, templates: t, topic: dt}, nil
}

// SendTemplatedMessageBatch sends the input message batch.
func (t *templatedSender) SendTemplatedMessageBatch(ctx context.Context, tms []*driver.TemplatedMessage) error {
	// Parse all templates.
	msgs := make([]*driver.Message, len(tms))
	for i, tm := range tms {
		msg, err := t.templates.Execute(tm)
		if err != nil {
			return err
		}

		// Validate the message.
		msgs[i] = msg
	}

	for _, msg := range msgs {
		if err := prepareAndSend(ctx, msg, t.s, t.topic); err != nil {
			return err
		}
	}
	return nil
}

func (t *templatedSender) ErrorAs(err error, as any) bool {
	// TODO implement me
	panic("implement me")
}

func (t *templatedSender) ErrorCode(err error) codes.Code {
	return errorCode(err)
}

func (t *templatedSender) As(as any) bool { return false }

func (t *templatedSender) Close() error {
	// TODO implement me
	panic("implement me")
}
