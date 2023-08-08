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
	"io"
	"mime"
	"os"
	"path/filepath"
	"time"
)

// Attachment is an interface used for email attachments.
type Attachment struct {
	// AttachmentID is the identifier of the attachment.
	AttachmentID string
	// ContentID is the attachment content identifier.
	// This value is optional, in case it is not set, a <filename> will be used.
	ContentID string
	// Filename is the attachment filename.
	File AttachmentFile
	// Filename is the attachment filename.
	Filename string
	// ContentType is the attachment content type.
	ContentType string
	// Inline determines if the attachment should be displayed inline.
	Inline bool
}

// AttachmentFile is an interface used for email attachment files.
// This value might be taken from *os.File, *blob.Reader, etc.
type AttachmentFile interface {
	io.Reader
	ContentType() string
	ModTime() time.Time
	Size() int64
	Close() error
}

// Attachments sets the email attachments.
func Attachments(attachments ...*Attachment) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		msg.Attachments = append(msg.Attachments, attachments...)
		return nil
	})
}

// Attach creates an attachment.
func Attach(att AttachmentFile, filename string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if att == nil {
			return errors.New("email: file is nil")
		}
		msg.Attachments = append(msg.Attachments, &Attachment{
			File:        att,
			Filename:    filename,
			ContentType: att.ContentType(),
			ContentID:   filename,
		})
		return nil
	})
}

func AttachFile(path string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}

		fa, err := newFileAttachment(path, f)
		if err != nil {
			return err
		}
		fileBase := filepath.Base(path)
		msg.Attachments = append(msg.Attachments, &Attachment{
			File:        fa,
			ContentID:   fileBase,
			Filename:    fileBase,
			ContentType: mime.TypeByExtension(filepath.Ext(path)),
		})
		return nil
	})
}

// EmbedAttachment creates an attachment that should be displayed inline.
func EmbedAttachment(att AttachmentFile, filename string) ComposeMsgFn {
	return composeFn(func(msg *messageOpts) error {
		if att == nil {
			return errors.New("email: file is nil")
		}
		if filename == "" {
			return errors.New("email: filename is empty")
		}

		msg.Attachments = append(msg.Attachments, &Attachment{
			File:        att,
			Filename:    filename,
			ContentType: att.ContentType(),
			Inline:      true,
		})
		return nil
	})
}

type fileAttachment struct {
	contentType string
	modTime     time.Time
	size        int64
	f           *os.File
}

func (a *fileAttachment) ContentType() string {
	return a.contentType
}

func (a *fileAttachment) ModTime() time.Time {
	return a.modTime
}

func (a *fileAttachment) Size() int64 {
	return a.size
}

func (a *fileAttachment) Read(p []byte) (n int, err error) {
	return a.f.Read(p)
}

func (a *fileAttachment) Close() error {
	return a.f.Close()
}

func newFileAttachment(path string, f *os.File) (*fileAttachment, error) {
	ct := mime.TypeByExtension(filepath.Ext(path))
	if ct == "" {
		ct = "application/octet-stream"
	}
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if stat.IsDir() {
		return nil, errors.New("email: file is a directory")
	}

	return &fileAttachment{
		contentType: ct,
		modTime:     stat.ModTime(),
		size:        stat.Size(),
		f:           f,
	}, nil
}
