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

package logemail_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"time"

	"golang.org/x/exp/slog"

	"github.com/blockysource/blocky-cloud/email"
	"github.com/blockysource/blocky-cloud/email/driver"
	"github.com/blockysource/blocky-cloud/email/logemail"
)

func ExampleOpenSender() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout))
	opts := logemail.SenderOptions{
		LogLevel: slog.LevelInfo,
		LogBody:  true,
	}
	s, err := logemail.OpenSender(logger, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	// Use s to send emails.
	msg, err := email.ComposeMessage(
		email.From("\"John Doe\" <john@doe.org>"),
		email.To("\"Jane Doe\" <jane@doe.org>"),
		email.Date(time.Now()),
		email.Subject("Hello!"),
		email.TextBody("Hey Jane! This is John. How are you?"),
		email.HTMLBody("<strong>Hey Jane!</strong><p>This is John. How are you?</p>"),
	)
	if err != nil {
		log.Fatal(err)
	}

	s.Send(context.Background(), msg)
}

func ExampleOpenSenderLogFn() {
	s, err := logemail.OpenSenderLogFn(func(ctx context.Context, msg *driver.Message) {
		// Log the message in custom way.
		log.Printf("msg: %+v", msg)
	})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	msg, err := email.ComposeMessage(
		email.From("\"John Doe\" <john@doe.org>"),
		email.To("\"Jane Doe\" <jane@doe.org>"),
		email.Date(time.Now()),
		email.Subject("Hello!"),
		email.TextBody("Hey Jane! This is John. How are you?"),
		email.HTMLBody("<strong>Hey Jane!</strong><p>This is John. How are you?</p>"),
	)
	if err != nil {
		log.Fatal(err)
	}

	s.Send(context.Background(), msg)
}

func ExampleURLOpener_OpenSenderURL() {
	// This example shows usage of the URLOpener with specific Logger implementation.
	logger := slog.New(slog.NewJSONHandler(os.Stdout))
	o := &logemail.URLOpener{Logger: logger}
	u := &url.URL{Scheme: logemail.Scheme, Host: ""}
	s, err := o.OpenSenderURL(context.Background(), u)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	msg, err := email.ComposeMessage(
		email.From("\"John Doe\" <john@doe.org>"),
		email.To("\"Jane Doe\" <jane@doe.org>"),
		email.Date(time.Now()),
		email.Subject("Hello!"),
		email.TextBody("Hey Jane! This is John. How are you?"),
		email.HTMLBody("<strong>Hey Jane!</strong><p>This is John. How are you?</p>"),
	)
	if err != nil {
		log.Fatal(err)
	}

	s.Send(context.Background(), msg)
}

func Example_openSenderFromURL() {
	ctx := context.Background()
	s, err := email.OpenSender(ctx, "log://?loglevel=debug&logbody=true")
	if err != nil {
		log.Fatal(err)
	}
	defer s.Shutdown(ctx)

	msg, err := email.ComposeMessage(
		email.From("\"John Doe\" <john@doe.org>"),
		email.To("\"Jane Doe\" <jane@doe.org>"),
		email.Date(time.Now()),
		email.Subject("Hello!"),
		email.TextBody("Hey Jane! This is John. How are you?"),
		email.HTMLBody("<strong>Hey Jane!</strong><p>This is John. How are you?</p>"),
	)
	if err != nil {
		log.Fatal(err)
	}

	err = s.Send(ctx, msg)
	if err != nil {
		log.Fatal(err)
	}
}

func ExampleOpenTemplatedSender() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout))
	opts := logemail.SenderOptions{
		LogLevel: slog.LevelInfo,
		LogBody:  true,
	}
	s, err := logemail.OpenTemplatedSender(logger, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	// Use s to send emails.
	msg, err := email.ComposeTemplatedMessage(
		email.From("\"John Doe\" <john@doe.com>"),
		email.To("\"Jane Doe\" <jane@doe.com>"),
		email.Subject("Hello, World!"),
		email.Template("welcome", "v1", map[string]any{"name": "John Doe"}),
	)
	if err != nil {
		log.Fatal(err)
	}

	s.Send(context.Background(), msg)
}

func ExampleOpenTemplatedSenderLogFn() {
	s, err := logemail.OpenTemplatedSenderLogFn(func(ctx context.Context, msg *driver.TemplatedMessage) {
		// Log the message in custom way.
		log.Printf("msg: %+v", msg)
	})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	// Use s to send emails.
	msg, err := email.ComposeTemplatedMessage(
		email.From("\"John Doe\" <john@doe.com>"),
		email.To("\"Jane Doe\" <jane@doe.com>"),
		email.Subject("Hello, World!"),
		email.Template("welcome", "v1", map[string]any{"name": "John Doe"}),
	)
	if err != nil {
		log.Fatal(err)
	}

	s.Send(context.Background(), msg)
}

func ExampleURLOpener_OpenTemplatedSenderURL() {
	// This example shows usage of the URLOpener with specific Logger implementation.
	logger := slog.New(slog.NewJSONHandler(os.Stdout))
	o := &logemail.URLOpener{Logger: logger}
	u := &url.URL{Scheme: logemail.Scheme, Host: ""}
	s, err := o.OpenTemplatedSenderURL(context.Background(), u)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	// Use s to send emails.
	msg, err := email.ComposeTemplatedMessage(
		email.From("\"John Doe\" <john@doe.com>"),
		email.To("\"Jane Doe\" <jane@doe.com>"),
		email.Subject("Hello, World!"),
		email.Template("welcome", "v1", map[string]any{"name": "John Doe"}),
	)
	if err != nil {
		log.Fatal(err)
	}

	s.Send(context.Background(), msg)
}

func Example_openTemplatedSenderFromURL() {
	ctx := context.Background()
	s, err := email.OpenTemplatedSender(ctx, "log://?loglevel=debug&logbody=true")
	if err != nil {
		log.Fatal(err)
	}
	defer s.Shutdown(ctx)

	// Use s to send emails.
	msg, err := email.ComposeTemplatedMessage(
		email.From("\"John Doe\" <john@doe.com>"),
		email.To("\"Jane Doe\" <jane@doe.com>"),
		email.Subject("Hello, World!"),
		email.Template("welcome", "v1", map[string]any{"name": "John Doe"}),
	)
	if err != nil {
		log.Fatal(err)
	}

	s.Send(context.Background(), msg)
}
