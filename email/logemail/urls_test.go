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
	"bytes"
	"context"
	"log"
	"testing"
	"time"

	"golang.org/x/exp/slog"

	"github.com/blockysource/blocky-cloud/email"
)

func TestOpenSender(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	date := time.Date(2023, 7, 27, 0, 19, 37, 19439767, time.FixedZone("CEST", 2*60*60))
	h := slog.HandlerOptions{ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == "time" {
			return slog.Attr{
				Key:   a.Key,
				Value: slog.TimeValue(date),
			}
		}
		return a
	}}.NewJSONHandler(buf)
	logger := slog.New(h)
	opts := SenderOptions{
		LogLevel: slog.LevelInfo,
		LogBody:  true,
	}
	s, err := OpenSender(logger, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	// Use s to send emails.
	msg, err := email.ComposeMessage(
		email.From("\"John Doe\" <john@doe.org>"),
		email.To("\"Jane Doe\" <jane@doe.org>"),
		email.Date(date),
		email.Subject("Hello!"),
		email.TextBody("Hey Jane! This is John. How are you?"),
		email.HTMLBody("<strong>Hey Jane!</strong><p>This is John. How are you?</p>"),
	)
	if err != nil {
		log.Fatal(err)
	}

	s.Send(context.Background(), msg)

	if buf.Len() == 0 {
		t.Fatal("expected buffer to have some content, got empty buffer")
	}

	const expected = `{"time":"2023-07-27T00:19:37.019439767+02:00","level":"INFO","msg":"sending message","message: ":{"messageID":"","from":{"Name":"John Doe","Address":"john@doe.org"},"to":[{"Name":"Jane Doe","Address":"jane@doe.org"}],"cc":null,"bcc":null,"subject":"Hello!","replyTo":null,"date":"2023-07-27T00:19:37.019439767+02:00","htmlBody":"<strong>Hey Jane!</strong><p>This is John. How are you?</p>","textBody":"Hey Jane! This is John. How are you?","attachment#0":{"contentID":"","filename":"hello.txt","contentType":"text/plain","inline":false}}}`
	if buf.String() != expected {
		log.Println(buf.String())
		t.Errorf("expected buffer to have %s, got %s", expected, buf.String())
	}
}
