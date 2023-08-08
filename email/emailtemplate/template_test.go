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

package emailtemplate

import (
	_ "embed"
	"fmt"
	"testing"

	"github.com/blockysource/blocky-cloud/email/driver"
)

//go:embed test.tmpl
var testTmpl []byte

func TestParseTemplate(t *testing.T) {
	tmpls, err := ParseTemplates(nil, &Template{
		Name: "test",
		Versions: []TemplateVersion{
			{
				Version: "1",
				Content: string(testTmpl),
				DefaultData: map[string]any{
					"from": map[string]any{
						"email": "john.doe@example.com",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Valid", func(t *testing.T) {
		const subject = "test subject"

		tm := driver.TemplatedMessage{
			From:         "",
			To:           []string{"jane.doe@example.com"},
			TemplateName: "test",
			TemplateData: map[string]any{
				"subject": subject,
				"from": map[string]any{
					"name":    "John",
					"surname": "Doe",
					"email":   "john.doe.2@example.com",
				},
				"body": "<h1>test</h1>",
			},
			TemplateVersion: "1",
		}

		msg, err := tmpls.Execute(&tm)
		if err != nil {
			t.Fatal(err)
		}

		expectedSubject := fmt.Sprintf("This is subject %s", subject)
		if msg.Subject != expectedSubject {
			t.Fatalf("expected subject to be %s, got %s", expectedSubject, msg.Subject)
		}

		if msg.From == "" {
			t.Fatal("expected from to be set")
		}

		const expectedFrom = "\"John Doe\" <john.doe.2@example.com>"
		if msg.From != expectedFrom {
			t.Fatalf("expected from to be %q, got %q", expectedFrom, msg.From)
		}

		const expectedHTML = "This is an example with <h1>test</h1>"
		if msg.HtmlBody != expectedHTML {
			t.Fatalf("expected html body to be %q, got %q", expectedHTML, msg.HtmlBody)
		}
	})

	t.Run("NoFrom", func(t *testing.T) {
		const subject = "test subject"

		tm := driver.TemplatedMessage{
			From:         "",
			To:           []string{"jane.doe@example.com"},
			TemplateName: "test",
			TemplateData: map[string]any{
				"subject": subject,
				"body":    "<h1>test</h1>",
			},
			TemplateVersion: "1",
		}

		msg, err := tmpls.Execute(&tm)
		if err != nil {
			t.Fatal(err)
		}

		expectedSubject := fmt.Sprintf("This is subject %s", subject)
		if msg.Subject != expectedSubject {
			t.Fatalf("expected subject to be %s, got %s", expectedSubject, msg.Subject)
		}

		// The default from should be used from template data.
		if msg.From == "" {
			t.Fatal("expected from to be set")
		}

		const expectedFrom = "<john.doe@example.com>"
		if msg.From != expectedFrom {
			t.Fatalf("expected from to be %q, got %q", expectedFrom, msg.From)
		}

		const expectedHTML = "This is an example with <h1>test</h1>"
		if msg.HtmlBody != expectedHTML {
			t.Fatalf("expected html body to be %q, got %q", expectedHTML, msg.HtmlBody)
		}
	})
}
