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
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"

	"github.com/blockysource/blocky-cloud/email/batcher"
	"github.com/blockysource/blocky-cloud/email/driver"
)

// TemplatedSender represents an implementation of the email template message sender.
type TemplatedSender struct {
	driver  driver.TemplatedSender
	mu      sync.Mutex
	batcher *batcher.BatcherG[*driver.TemplatedMessage]
	closed  bool
	err     error
	cancel  func()
}

// NewTemplatedSender is intended for use by drivers only. Do not use in application code.
var NewTemplatedSender = newTemplatedSender

func newTemplatedSender(d driver.TemplatedSender, opts *batcher.Options) *TemplatedSender {
	ctx, cancel := context.WithCancel(context.Background())
	mp := &TemplatedSender{
		driver: d,
		cancel: cancel,
	}
	mp.batcher = newTemplatedSenderBatcher(ctx, mp, d, opts)

	_, file, lineno, ok := runtime.Caller(1)
	runtime.SetFinalizer(mp, func(c *TemplatedSender) {
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if !closed {
			var caller string
			if ok {
				caller = fmt.Sprintf(" (%s:%d)", file, lineno)
			}
			log.Printf("A emails.TemplatedSender was never closed%s", caller)
		}
	})
	return mp
}

func newTemplatedSenderBatcher(ctx context.Context, s *TemplatedSender, dt driver.TemplatedSender, opts *batcher.Options) *batcher.BatcherG[*driver.TemplatedMessage] {
	handler := func(items []*driver.TemplatedMessage) error {
		return dt.SendTemplatedMessageBatch(ctx, items)
	}
	return batcher.New[*driver.TemplatedMessage](opts, handler, nil)
}

// Send uses the mailing provider to send the input message.
func (p *TemplatedSender) Send(ctx context.Context, m *TemplatedMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	err := p.err
	if err != nil {
		return err
	}

	dm := &driver.TemplatedMessage{
		MessageID:       m.MessageID,
		From:            m.From,
		To:              m.To,
		Cc:              m.Cc,
		Bcc:             m.Bcc,
		HtmlBody:        m.HtmlBody,
		TextBody:        m.TextBody,
		Subject:         m.Subject,
		Date:            m.Date,
		ReplyTo:         m.ReplyTo,
		Attachments:     make([]*driver.Attachment, len(m.Attachments)),
		TemplateName:    m.TemplateName,
		TemplateData:    m.TemplateData,
		TemplateVersion: m.TemplateVersion,
		BeforeSend:      m.BeforeSend,
		AfterSend:       m.AfterSend,
	}

	for i, a := range m.Attachments {
		dm.Attachments[i] = &driver.Attachment{
			ContentID:   a.ContentID,
			File:        a.File,
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Inline:      a.Inline,
		}
	}

	return p.batcher.Add(ctx, dm)
}

// Shutdown flushes pending messages and disconnects the TemplatedSender.
// It only returns after all pending messages have been sent.
func (p *TemplatedSender) Shutdown(ctx context.Context) (err error) {
	p.mu.Lock()
	if p.closed {
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	c := make(chan struct{})
	go func() {
		defer close(c)
		p.batcher.Shutdown()
	}()

	select {
	case <-c:
	case <-ctx.Done():
	}
	p.cancel()

	if err = p.driver.Close(); err != nil {
		return err
	}

	return ctx.Err()
}

// As converts i to driver-specific types.
func (p *TemplatedSender) As(i any) bool {
	return p.driver.As(i)
}

// ErrorAs converts err to driver-specific types.
// It panics if i is nil or not a pointer.
// It returns false if err == nil.
func (p *TemplatedSender) ErrorAs(err error, i any) bool {
	return errorsAs(err, i, p.driver.ErrorAs)
}
