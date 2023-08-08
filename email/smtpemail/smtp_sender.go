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
	"fmt"
	"io"
	"net/smtp"
	"net/textproto"
	"strconv"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/blockysource/blocky-cloud/email/driver"
)

type smtpSender struct {
	c    *smtp.Client
	d    *Dialer
	opts SenderOptions
}

func (s *smtpSender) redial(ctx context.Context) error {
	c, err := s.d.dial(ctx)
	if err != nil {
		return err
	}
	s.c = c
	return nil
}

func (s *smtpSender) sendMessage(ctx context.Context, msg *message) (*driver.DeliveryEvent, error) {
	var retries = 3
	for retries > 0 {
		if err := s.c.Mail(msg.from); err != nil {
			if err == io.EOF {
				if err = s.redial(ctx); err != nil {
					return nil, err

				}
				retries--
				continue
			}
			// Reset the connection for further use.
			s.c.Reset()
			return nil, err
		}
		break
	}
	if retries == 0 {
		return nil, errors.New("smtpemail: failed to establish connection")
	}

	var bt driver.BounceType
	var bouncedRecipients []driver.BouncedRecipient
	for _, to := range msg.to {
		// Set the recipients.
		if err := s.c.Rcpt(to); err != nil {
			var protoErr *textproto.Error
			if errors.As(err, &protoErr) {
				cs := bounceReason(strconv.Itoa(protoErr.Code))
				sc := extractMsgBounceReason(protoErr.Msg)

				status := cs
				if cs.compare(sc) == lessSpecific {
					status = sc
				}

				res, _ := bounceStatusMap[status]
				if driver.BounceType(res.Type) > bt {
					bt = driver.BounceType(res.Type)
				}

				bouncedRecipients = append(bouncedRecipients, driver.BouncedRecipient{
					Email:          to,
					DiagnosticCode: fmt.Sprintf("smtp: %d %s", protoErr.Code, protoErr.Msg),
					Status:         string(status),
				})
				continue
			}
			// Reset the conenection.
			s.c.Reset()
			return nil, err
		}
	}

	if len(bouncedRecipients) > 0 {
		now := time.Now()
		de := &driver.DeliveryEvent{
			Type: driver.DeliveryEventTypeBounce,
			Bounce: driver.BounceRecord{
				Recipients: bouncedRecipients,
				BounceType: bt,
				Timestamp:  now,
			},
			Timestamp: now,
			MessageID: msg.id,
		}
		// Reset the connection for further use.
		if err := s.c.Reset(); err != nil {
			return nil, err
		}
		return de, nil
	}

	w, err := s.c.Data()
	if err != nil {
		// Reset the connection for further use.
		s.c.Reset()
		return nil, err
	}

	msg.charset = s.opts.Charset
	if msg.charset == "" {
		msg.charset = "utf-8"
	}
	msg.encoding = s.opts.Encoding

	if _, err = msg.WriteTo(w); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		// Reset the connection for further use.
		s.c.Reset()
		return nil, err
	}

	de := &driver.DeliveryEvent{
		Type:      driver.DeliveryEventTypeSend,
		Timestamp: time.Now(),
		MessageID: msg.id,
	}
	return de, nil
}

func extractMsgBounceReason(msg string) bounceReason {
	// Check if the message is of format.
	// %d.%d.%d %s
	// The first char might be a space sometimes.
	var code []rune
	var hasDigit bool
	for _, r := range msg {
		if r == ' ' && !hasDigit {
			continue
		}
		if unicode.IsDigit(r) {
			hasDigit = true
			code = append(code, r)
			continue
		}
		if r == '.' && hasDigit {
			code = append(code, r)
			continue
		}

		if len(code) > 0 {
			break
		}
		return bounceCodeNotFound
	}
	return bounceReason(code)
}

// senderPool is a pool of SMTP senders.
type senderPool struct {
	clients  chan *smtpSender
	ctx      context.Context
	cancel   context.CancelFunc
	isClosed atomic.Bool
}

func newSenderPool(ctx context.Context, d *Dialer, opts SenderOptions) (*senderPool, error) {
	pool := &senderPool{
		clients: make(chan *smtpSender, opts.MaxHandlers),
	}
	context.WithCancel(ctx)
	if err := pool.fill(ctx, d, opts); err != nil {
		return nil, err
	}
	return pool, nil
}

func (p *senderPool) fill(ctx context.Context, d *Dialer, opts SenderOptions) error {
	for i := 0; i < cap(p.clients); i++ {
		c, err := d.dial(ctx)
		if err != nil {
			return err
		}
		s := &smtpSender{
			d:    d,
			opts: opts,
			c:    c,
		}

		p.clients <- s
	}
	return nil
}

func (p *senderPool) get() (*smtpSender, bool) {
	select {
	case <-p.ctx.Done():
		return nil, false
	case c, ok := <-p.clients:
		if !ok {
			return nil, false
		}
		return c, true
	}
}

func (p *senderPool) put(c *smtpSender) {
	p.clients <- c
}

func (p *senderPool) close(ctx context.Context) error {
	if !p.isClosed.CompareAndSwap(false, true) {
		return errors.New("smtpemail: pool is already closed")
	}
	p.cancel()

	for {
		select {
		case <-ctx.Done():
			return nil
		case c := <-p.clients:
			if err := c.c.Close(); err != nil {
				return err
			}
		}
	}
}
