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
	"bytes"
	"io"
	"sync"
	"time"

	"github.com/blockysource/blocky-cloud/email"
	"github.com/blockysource/blocky-cloud/email/driver"
)

// message represents an email.
type message struct {
	id   string
	from string
	to   []string

	header      header
	parts       []*part
	attachments []*file
	embedded    []*file
	charset     string
	encoding    Encoding
	hEncoder    mimeEncoder
	buf         bytes.Buffer
}

type header map[string][]string

type part struct {
	contentType   string
	contentWriter stringWriterTo
	encoding      Encoding
}

var msgPool = &sync.Pool{}

func getMessage() *message {
	if v := msgPool.Get(); v != nil {
		return v.(*message)
	}
	return newMessage()
}

func putMessage(m *message) {
	m.reset()
	msgPool.Put(m)
}

// newMessage creates a new message. It uses UTF-8 and quoted-printable encoding
// by default.
func newMessage() *message {
	return &message{
		header:   make(header),
		charset:  "UTF-8",
		encoding: QuotedPrintable,
	}
}

// reset resets the message so it can be reused.
func (m *message) reset() {
	for k := range m.header {
		delete(m.header, k)
	}
	m.parts = m.parts[:0]
	m.attachments = m.attachments[:0]
	m.embedded = m.embedded[:0]
}

// setHeader sets a value to the given header field.
func (m *message) setHeader(field string, value ...string) {
	m.encodeHeader(value)
	m.header[field] = value
}

func (m *message) encodeHeader(values []string) {
	for i := range values {
		values[i] = m.encodeString(values[i])
	}
}

func (m *message) encodeString(value string) string {
	return m.hEncoder.Encode(m.charset, value)
}

// setHeaders sets the message headers.
func (m *message) setHeaders(h map[string][]string) {
	for k, v := range h {
		m.setHeader(k, v...)
	}
}

// setAddressHeader sets an address to the given header field.
func (m *message) setAddressHeader(field string, addresses ...string) error {
	addrHeader := make([]string, len(addresses))
	for i, addr := range addresses {
		a, err := email.ParseAddress(addr)
		if err != nil {
			return err
		}
		addrHeader[i] = m.formatAddress(a.Address, a.Name)
	}
	m.header[field] = addrHeader
	return nil
}

// formatAddress formats an address and a name as a valid RFC 5322 address.
func (m *message) formatAddress(address, name string) string {
	if name == "" {
		return address
	}

	enc := m.encodeString(name)
	if enc == name {
		m.buf.WriteByte('"')
		for i := 0; i < len(name); i++ {
			b := name[i]
			if b == '\\' || b == '"' {
				m.buf.WriteByte('\\')
			}
			m.buf.WriteByte(b)
		}
		m.buf.WriteByte('"')
	} else if hasSpecials(name) {
		m.buf.WriteString(bEncoding.Encode(m.charset, name))
	} else {
		m.buf.WriteString(enc)
	}
	m.buf.WriteString(" <")
	m.buf.WriteString(address)
	m.buf.WriteByte('>')

	addr := m.buf.String()
	m.buf.Reset()
	return addr
}

func hasSpecials(text string) bool {
	for i := 0; i < len(text); i++ {
		switch c := text[i]; c {
		case '(', ')', '<', '>', '[', ']', ':', ';', '@', '\\', ',', '.', '"':
			return true
		}
	}

	return false
}

// setDateHeader sets a date to the given header field.
func (m *message) setDateHeader(field string, date time.Time) {
	m.header[field] = []string{date.Format(time.RFC1123Z)}
}

// getHeader gets a header field.
func (m *message) getHeader(field string) []string {
	return m.header[field]
}

// setBody sets the body of the message. It replaces any content previously set
// by setBody, addAlternative or addAlternativeWriter.
func (m *message) setBody(contentType, body string, settings ...partSetting) {
	m.parts = []*part{m.newPart(contentType, stringWriterTo(body), settings)}
}

// addAlternative adds an alternative part to the message.
//
// It is commonly used to send HTML emails that default to the plain text
// version for backward compatibility. addAlternative appends the new part to
// the end of the message. So the plain text part should be added before the
// HTML part. See http://en.wikipedia.org/wiki/MIME#Alternative
func (m *message) addAlternative(contentType, body string, settings ...partSetting) {
	m.addAlternativeWriter(contentType, stringWriterTo(body), settings...)
}

type stringWriterTo string

func (s stringWriterTo) WriteTo(w io.Writer) (int64, error) {
	n, err := io.WriteString(w, string(s))
	if err != nil {
		return int64(n), err
	}
	return int64(n), nil
}

// addAlternativeWriter adds an alternative part to the message. It can be
// useful with the text/template or html/template packages.
func (m *message) addAlternativeWriter(contentType string, cw stringWriterTo, settings ...partSetting) {
	m.parts = append(m.parts, m.newPart(contentType, cw, settings))
}

func (m *message) newPart(contentType string, cw stringWriterTo, settings []partSetting) *part {
	p := &part{
		contentType:   contentType,
		contentWriter: cw,
		encoding:      m.encoding,
	}

	for _, s := range settings {
		s(p)
	}

	return p
}

// A partSetting can be used as an argument in message.SetBody,
// Message.addAlternative or message.addAlternativeWriter to configure the part
// added to a message.
type partSetting func(*part)

// setPartEncoding sets the encoding of the part added to the message. By
// default, parts use the same encoding than the message.
func setPartEncoding(e Encoding) partSetting {
	return partSetting(func(p *part) {
		p.encoding = e
	})
}

type file struct {
	name     string
	header   map[string][]string
	toWriter io.WriterTo
}

func (f *file) setHeader(field, value string) {
	f.header[field] = []string{value}
}

func (m *message) appendFile(list []*file, att *driver.Attachment) []*file {
	f := &file{
		name:     att.Filename,
		header:   make(map[string][]string),
		toWriter: att,
	}

	if att.ContentType != "" {
		f.setHeader("Content-Type", att.ContentType)
	}
	if att.ContentID != "" {
		f.setHeader("Content-ID", "<"+att.ContentID+">")
	}

	if list == nil {
		return []*file{f}
	}

	return append(list, f)
}

// attach attaches the files to the email.
func (m *message) attach(att *driver.Attachment) {
	m.attachments = m.appendFile(m.attachments, att)
}

// embed embeds the images to the email.
func (m *message) embed(att *driver.Attachment) {
	m.embedded = m.appendFile(m.embedded, att)
}

func (m *message) prepare(msg *driver.Message) error {
	m.id = msg.MessageID
	m.from = msg.From
	m.to = getRecipients(msg)
	if err := m.setAddressHeader("From", msg.From); err != nil {
		return err
	}

	for _, to := range msg.To {
		if err := m.setAddressHeader("To", to); err != nil {
			return err
		}
	}

	if len(msg.Cc) > 0 {
		for _, cc := range msg.Cc {
			if err := m.setAddressHeader("Cc", cc); err != nil {
				return err
			}
		}
	}

	m.setHeader("Subject", msg.Subject)

	if msg.HtmlBody != "" {
		m.setBody("text/html", msg.HtmlBody)
	}

	if msg.HtmlBody != "" && msg.TextBody != "" {
		m.addAlternative("text/plain", msg.TextBody)
	} else if msg.TextBody != "" {
		m.setBody("text/plain", msg.TextBody)
	}

	for _, att := range msg.Attachments {
		if att.Inline {
			m.embed(att)
		} else {
			m.attach(att)
		}
	}

	if msg.ReplyTo != "" {
		if err := m.setAddressHeader("Reply-To", msg.ReplyTo); err != nil {
			return err
		}
	}

	if msg.MessageID != "" {
		m.setHeader("Message-ID", msg.MessageID)
	}
	return nil
}

func getRecipients(m *driver.Message) []string {
	n := len(m.To) + len(m.Cc) + len(m.Bcc)
	list := make([]string, 0, n)

	list = append(list, m.To...)
	list = append(list, m.Cc...)
	list = append(list, m.Bcc...)
	return list
}
