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
	"reflect"
	"runtime"
	"sync"

	"github.com/blockysource/blocky-cloud/email/batcher"
	"github.com/blockysource/blocky-cloud/email/driver"
)

// Sender represents an implementation of the email message s.
type Sender struct {
	driver  driver.Sender
	mu      sync.Mutex
	batcher *batcher.BatcherG[*driver.Message]
	closed  bool
	err     error
	cancel  func()
}

// NewSender is intended for use by drivers only. Do not use in application code.
var NewSender = newSender

func newSender(d driver.Sender, opts *batcher.Options) *Sender {
	ctx, cancel := context.WithCancel(context.Background())
	mp := &Sender{
		driver: d,
		cancel: cancel,
	}
	mp.batcher = newSenderBatcher(ctx, mp, d, opts)

	_, file, lineno, ok := runtime.Caller(1)
	runtime.SetFinalizer(mp, func(c *Sender) {
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if !closed {
			var caller string
			if ok {
				caller = fmt.Sprintf(" (%s:%d)", file, lineno)
			}
			log.Printf("A emails.Sender was never closed%s", caller)
		}
	})
	return mp
}

func newSenderBatcher(ctx context.Context, s *Sender, dt driver.Sender, opts *batcher.Options) *batcher.BatcherG[*driver.Message] {
	handler := func(items []*driver.Message) error {
		return dt.SendBatch(ctx, items)
	}
	return batcher.New[*driver.Message](opts, handler, func(msg *driver.Message) int {
		var n int
		if msg.HtmlBody != "" {
			n++
		}
		if msg.TextBody != "" {
			n++
		}
		for _, a := range msg.Attachments {
			n += int(a.File.Size())
		}
		return n
	})
}

// Send uses the mailing provider to send the input message.
func (p *Sender) Send(ctx context.Context, m *Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	err := p.err
	if err != nil {
		return err
	}

	dm := &driver.Message{
		MessageID:   m.MessageID,
		From:        m.From,
		To:          m.To,
		Cc:          m.Cc,
		Bcc:         m.Bcc,
		HtmlBody:    m.HtmlBody,
		TextBody:    m.TextBody,
		Subject:     m.Subject,
		Date:        m.Date,
		ReplyTo:     m.ReplyTo,
		Attachments: make([]*driver.Attachment, len(m.Attachments)),
		BeforeSend:  m.BeforeSend,
		AfterSend:   m.AfterSend,
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

// Shutdown flushes pending messages and disconnects the Sender.
// It only returns after all pending messages have been sent.
func (p *Sender) Shutdown(ctx context.Context) (err error) {
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
func (p *Sender) As(i any) bool {
	return p.driver.As(i)
}

// ErrorAs converts err to driver-specific types.
// It panics if i is nil or not a pointer.
// It returns false if err == nil.
func (p *Sender) ErrorAs(err error, i any) bool {
	return errorsAs(err, i, p.driver.ErrorAs)
}

// errorsAs is a helper for the errorsAs method of an API's portable type.
// It performs some initial nil checks, and does a single level of unwrapping
// when err is a *gcerr.Error. Then it calls its errorAs argument, which should
// be a driver implementation of errorsAs.
func errorsAs(err error, target any, errorAs func(error, any) bool) bool {
	if err == nil {
		return false
	}
	if target == nil {
		panic("ErrorAs target cannot be nil")
	}
	val := reflect.ValueOf(target)
	if val.Type().Kind() != reflect.Ptr || val.IsNil() {
		panic("ErrorAs target must be a non-nil pointer")
	}

	return errorAs(err, target)
}
