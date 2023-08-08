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
	"bufio"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var _bufioWriterPool = sync.Pool{}

func newBufioWriter(w io.Writer) *bufio.Writer {
	if v := _bufioWriterPool.Get(); v != nil {
		bw := v.(*bufio.Writer)
		bw.Reset(w)
		return bw
	}
	return bufio.NewWriter(w)
}

func putBufioWriter(bw *bufio.Writer) {
	bw.Reset(nil)
	_bufioWriterPool.Put(bw)
}

// messageWriter is a message writer.
type messageWriter struct {
	w *bufio.Writer

	n          int64
	writers    [3]*multipart.Writer
	partWriter io.Writer
	depth      uint8
	err        error
}

func (m *message) WriteTo(w io.Writer) (n int64, err error) {
	bw := newBufioWriter(w)
	defer putBufioWriter(bw)

	mw := messageWriter{w: bw}
	mw.writeMessage(m)

	if mw.err != nil {
		return mw.n, mw.err
	}

	if err = bw.Flush(); err != nil {
		return mw.n, err
	}
	return mw.n, err
}

func (mw *messageWriter) writeMessage(m *message) {
	if _, ok := m.header["Mime-Version"]; !ok {
		mw.writeString("Mime-Version: 1.0\r\n")
	}
	if _, ok := m.header["Date"]; !ok {
		mw.writeHeader("Date", now().Format(time.RFC1123Z))
	}

	mw.writeHeaders(m.header)

	if m.hasMixedPart() {
		mw.openMultipart("mixed")
	}

	if m.hasRelatedPart() {
		mw.openMultipart("related")
	}

	if m.hasAlternativePart() {
		mw.openMultipart("alternative")
	}
	for _, p := range m.parts {
		mw.writePart(p, m.charset)
	}
	if m.hasAlternativePart() {
		mw.closeMultipart()
	}

	mw.addFiles(m.embedded, false)
	if m.hasRelatedPart() {
		mw.closeMultipart()
	}

	mw.addFiles(m.attachments, true)
	if m.hasMixedPart() {
		mw.closeMultipart()
	}
}

func (m *message) hasMixedPart() bool {
	return (len(m.parts) > 0 && len(m.attachments) > 0) || len(m.attachments) > 1
}

func (m *message) hasRelatedPart() bool {
	return (len(m.parts) > 0 && len(m.embedded) > 0) || len(m.embedded) > 1
}

func (m *message) hasAlternativePart() bool {
	return len(m.parts) > 1
}

func (mw *messageWriter) writeString(s string) {
	n, _ := mw.w.WriteString(s)
	mw.n += int64(n)
}

func (mw *messageWriter) addFiles(files []*file, isAttachment bool) {
	for _, f := range files {
		if _, ok := f.header["Content-Type"]; !ok {
			mediaType := mime.TypeByExtension(filepath.Ext(f.name))
			if mediaType == "" {
				mediaType = "application/octet-stream"
			}
			f.setHeader("Content-Type", mediaType+`; name="`+f.name+`"`)
		}

		if _, ok := f.header["Content-Transfer-Encoding"]; !ok {
			f.setHeader("Content-Transfer-Encoding", string(Base64))
		}

		if _, ok := f.header["Content-Disposition"]; !ok {
			var disp string
			if isAttachment {
				disp = "attachment"
			} else {
				disp = "inline"
			}
			f.setHeader("Content-Disposition", disp+`; filename="`+f.name+`"`)
		}

		if !isAttachment {
			if _, ok := f.header["Content-ID"]; !ok {
				f.setHeader("Content-ID", "<"+f.name+">")
			}
		}
		mw.writeHeaders(f.header)
		mw.writeBody(f.toWriter, Base64)
	}
}

func (mw *messageWriter) openMultipart(mimeType string) {
	mpw := multipart.NewWriter(mw.w)
	contentType := "multipart/" + mimeType + ";\r\n boundary=" + mpw.Boundary()
	mw.writers[mw.depth] = mpw

	if mw.depth == 0 {
		mw.writeHeader("Content-Type", contentType)
		mw.writeString("\r\n")
	} else {
		mw.createPart(map[string][]string{
			"Content-Type": {contentType},
		})
	}
	mw.depth++
}

func (mw *messageWriter) createPart(h map[string][]string) {
	mw.partWriter, mw.err = mw.writers[mw.depth-1].CreatePart(h)
}

func (mw *messageWriter) closeMultipart() {
	if mw.depth > 0 {
		mw.writers[mw.depth-1].Close()
		mw.depth--
	}
}

func (mw *messageWriter) writePart(p *part, charset string) {
	mw.writeHeaders(map[string][]string{
		"Content-Type":              {p.contentType + "; charset=" + charset},
		"Content-Transfer-Encoding": {string(p.encoding)},
	})
	mw.writeBody(p.contentWriter, p.encoding)
}

func (mw *messageWriter) writeHeader(k string, v ...string) {
	mw.writeString(k)
	if len(v) == 0 {
		mw.writeString(":\r\n")
		return
	}
	mw.writeString(": ")

	// Max header line length is 78 characters in RFC 5322 and 76 characters
	// in RFC 2047. So for the sake of simplicity we use the 76 characters
	// limit.
	charsLeft := 76 - len(k) - len(": ")

	for i, s := range v {
		// If the line is already too long, insert a newline right away.
		if charsLeft < 1 {
			if i == 0 {
				mw.writeString("\r\n ")
			} else {
				mw.writeString(",\r\n ")
			}
			charsLeft = 75
		} else if i != 0 {
			mw.writeString(", ")
			charsLeft -= 2
		}

		// While the header content is too long, fold it by inserting a newline.
		for len(s) > charsLeft {
			s = mw.writeLine(s, charsLeft)
			charsLeft = 75
		}
		mw.writeString(s)
		if i := lastIndexByte(s, '\n'); i != -1 {
			charsLeft = 75 - (len(s) - i - 1)
		} else {
			charsLeft -= len(s)
		}
	}
	mw.writeString("\r\n")
}

func (mw *messageWriter) writeLine(s string, charsLeft int) string {
	// If there is already a newline before the limit. Write the line.
	if i := strings.IndexByte(s, '\n'); i != -1 && i < charsLeft {
		mw.writeString(s[:i+1])
		return s[i+1:]
	}

	for i := charsLeft - 1; i >= 0; i-- {
		if s[i] == ' ' {
			mw.writeString(s[:i])
			mw.writeString("\r\n ")
			return s[i+1:]
		}
	}

	// We could not insert a newline cleanly so look for a space or a newline
	// even if it is after the limit.
	for i := 75; i < len(s); i++ {
		if s[i] == ' ' {
			mw.writeString(s[:i])
			mw.writeString("\r\n ")
			return s[i+1:]
		}
		if s[i] == '\n' {
			mw.writeString(s[:i+1])
			return s[i+1:]
		}
	}

	// Too bad, no space or newline in the whole string. Just write everything.
	mw.writeString(s)
	return ""
}

func (mw *messageWriter) writeHeaders(h map[string][]string) {
	if mw.depth == 0 {
		for k, v := range h {
			if k != "Bcc" {
				mw.writeHeader(k, v...)
			}
		}
	} else {
		mw.createPart(h)
	}
}

func (mw *messageWriter) writeBody(toWriter io.WriterTo, enc Encoding) {
	var subWriter io.Writer
	if mw.depth == 0 {
		mw.writeString("\r\n")
		subWriter = mw.w
	} else {
		subWriter = mw.partWriter
	}

	if enc == Base64 {
		wc := base64.NewEncoder(base64.StdEncoding, newBase64LineWriter(subWriter))
		_, mw.err = toWriter.WriteTo(wc)
		wc.Close()
	} else if enc == Unencoded {
		_, mw.err = toWriter.WriteTo(subWriter)
	} else {
		wc := newQPWriter(subWriter)
		_, mw.err = toWriter.WriteTo(wc)
		wc.Close()
	}

	if c, ok := toWriter.(io.Closer); ok {
		c.Close()
	}

}

// As required by RFC 2045, 6.7. (page 21) for quoted-printable, and
// RFC 2045, 6.8. (page 25) for base64.
const maxLineLen = 76

// base64LineWriter limits text encoded in base64 to 76 characters per line
type base64LineWriter struct {
	w       io.Writer
	lineLen int
}

func newBase64LineWriter(w io.Writer) *base64LineWriter {
	return &base64LineWriter{w: w}
}

func (w *base64LineWriter) Write(p []byte) (int, error) {
	n := 0
	for len(p)+w.lineLen > maxLineLen {
		w.w.Write(p[:maxLineLen-w.lineLen])
		w.w.Write([]byte("\r\n"))
		p = p[maxLineLen-w.lineLen:]
		n += maxLineLen - w.lineLen
		w.lineLen = 0
	}

	w.w.Write(p)
	w.lineLen += len(p)

	return n + len(p), nil
}

// Stubbed out for testing.
var now = time.Now
