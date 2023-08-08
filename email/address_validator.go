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
	"sync"

	"github.com/blockysource/blocky-cloud/email/batcher"
	"github.com/blockysource/blocky-cloud/email/driver"
)

// AddressValidator is an email address validator implementation.
type AddressValidator struct {
	driver  driver.AddressValidator
	mu      sync.Mutex
	batcher *batcher.BatcherG[*driver.Address]
	closed  bool
	err     error
	cancel  func()
}

// // NewAddressValidator is intended for use by drivers only. Do not use in application code.
// var NewAddressValidator = newAddressValidator
//
// func newAddressValidator(d driver.AddressValidator, opts *batcher.Options) *AddressValidator {
// 	ctx, cancel := context.WithCancel(context.Background())
// 	mp := &AddressValidator{
// 		driver: d,
// 		cancel: cancel,
// 	}
// 	mp.batcher = newAddressValidatorBatcher(ctx, mp, d, opts)
//
// 	_, file, lineno, ok := runtime.Caller(1)
// 	runtime.SetFinalizer(mp, func(c *AddressValidator) {
// 		c.mu.Lock()
// 		closed := c.closed
// 		c.mu.Unlock()
// 		if !closed {
// 			var caller string
// 			if ok {
// 				caller = fmt.Sprintf(" (%s:%d)", file, lineno)
// 			}
// 			log.Printf("A emails.AddressValidator was never closed%s", caller)
// 		}
// 	})
// 	return mp
// }
//
// func newAddressValidatorBatcher(ctx context.Context, s *AddressValidator, dt driver.AddressValidator, opts *batcher.Options) *batcher.BatcherG[*driver.Address] {
// 	handler := func(items []*driver.Address) error {
// 		return dt.ValidateAddressBatch(ctx, items)
// 	}
// 	return batcher.New[*driver.Address](opts, handler, nil)
// }
//
// // Validate uses the mailing provider to send the input message.
// func (p *AddressValidator) Validate(ctx context.Context, m *Address) error {
// 	if err := ctx.Err(); err != nil {
// 		return err
// 	}
// 	p.mu.Lock()
// 	defer p.mu.Unlock()
//
// 	err := p.err
// 	if err != nil {
// 		return err
// 	}
//
// 	dm := &driver.Address{
// 		MessageID:       m.MessageID,
// 		From:            m.From,
// 		To:              m.To,
// 		Cc:              m.Cc,
// 		Bcc:             m.Bcc,
// 		HtmlBody:        m.HtmlBody,
// 		TextBody:        m.TextBody,
// 		Subject:         m.Subject,
// 		Date:            m.Date,
// 		ReplyTo:         m.ReplyTo,
// 		Attachments:     make([]*driver.Attachment, len(m.Attachments)),
// 		TemplateName:    m.TemplateName,
// 		TemplateData:    m.TemplateData,
// 		TemplateVersion: m.TemplateVersion,
// 	}
//
// 	for i, a := range m.Attachments {
// 		dm.Attachments[i] = &driver.Attachment{
// 			ContentID:   a.ContentID,
// 			File:        a.File,
// 			Filename:    a.Filename,
// 			ContentType: a.ContentType,
// 			Inline:      a.Inline,
// 		}
// 	}
//
// 	return p.batcher.Add(ctx, dm)
// }
//
// // Shutdown flushes pending messages and disconnects the AddressValidator.
// // It only returns after all pending messages have been sent.
// func (p *AddressValidator) Shutdown(ctx context.Context) (err error) {
// 	p.mu.Lock()
// 	if p.closed {
// 		return nil
// 	}
// 	p.closed = true
// 	p.mu.Unlock()
//
// 	c := make(chan struct{})
// 	go func() {
// 		defer close(c)
// 		p.batcher.Shutdown()
// 	}()
//
// 	select {
// 	case <-c:
// 	case <-ctx.Done():
// 	}
// 	p.cancel()
//
// 	if err = p.driver.Close(); err != nil {
// 		return err
// 	}
//
// 	return ctx.Err()
// }
//
// // As converts i to driver-specific types.
// func (p *AddressValidator) As(i any) bool {
// 	return p.driver.As(i)
// }
//
// // ErrorAs converts err to driver-specific types.
// // It panics if i is nil or not a pointer.
// // It returns false if err == nil.
// func (p *AddressValidator) ErrorAs(err error, i any) bool {
// 	return errorsAs(err, i, p.driver.ErrorAs)
// }
