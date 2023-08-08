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
	"testing"
)

func TestComposeTemplateMessage(t *testing.T) {
	tm, err := ComposeTemplatedMessage(
		MessageID("1234567890"),
		From("\"Jacek Kucharczyk\" <jacek@smartlycode.pl>"),
		To("\"John Doe\" <john@doe.com>"),
		Cc("\"Jill Doe\" <jill@doe.com>", "Jane Doe <jane@doe.com>"),
		Bcc("\"Jack Doe\" <jack@doe.com>"),
		Subject("Hello, World!"),
		Template("welcome", "v1", map[string]any{"name": "John Doe"}),
		ReplyTo("\"Jacek Kucharczyk\" <jacek@smartlycode.pl>"),
	)

	if err != nil {
		t.Fatal(err)
	}

	if tm.MessageID != "1234567890" {
		t.Errorf("expected message ID to be 1234567890, got %s", tm.MessageID)
	}

	if tm.From != "\"Jacek Kucharczyk\" <jacek@smartlycode.pl>" {
		t.Errorf("expected from to be Jacek Kucharczyk <jacek@smartlycode.pl>, got %s", tm.From)
	}

	if len(tm.To) != 1 {
		t.Errorf("expected to have 1 recipient, got %d", len(tm.To))
	}

	if tm.To[0] != "\"John Doe\" <john@doe.com>" {
		t.Errorf("expected to have John Doe <john@doe.com> as recipient, got %s", tm.To[0])
	}

	if len(tm.Cc) != 2 {
		t.Errorf("expected to have 2 carbon copy recipients, got %d", len(tm.Cc))
	} else {
		if tm.Cc[0] != "\"Jill Doe\" <jill@doe.com>" {
			t.Errorf("expected to have Jill Doe <jill@doe.com> as carbon copy recipient, got %s", tm.Cc[0])
		}

		if tm.Cc[1] != "Jane Doe <jane@doe.com>" {
			t.Errorf("expected to have Jane Doe <jane@doe.com> as carbon copy recipient, got %s", tm.Cc[1])
		}
	}

}
