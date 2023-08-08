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
	"net/url"

	"github.com/blockysource/go-pkg/urlopener"
)

// SenderURLOpener represents types that can open Sender based on a URL.
type SenderURLOpener interface {
	OpenSenderURL(ctx context.Context, u *url.URL) (*Sender, error)
}

// TemplatedSenderURLOpener represents types that can open TemplatedSender based on a URL.
type TemplatedSenderURLOpener interface {
	OpenTemplatedSenderURL(ctx context.Context, u *url.URL) (*TemplatedSender, error)
}

// URLMux is URL opener multiplexer. It matches the scheme of the URLs against
// a set of registered schemes and calls the opener that matches the URL's
// scheme. See https://gocloud.dev/concepts/urls/ for more information.
type URLMux struct {
	senderSchemes          urlopener.SchemeMap
	templatedSenderSchemes urlopener.SchemeMap
}

// SenderSchemes returns a sorted slice of the registered Sender schemes.
func (mux *URLMux) SenderSchemes() []string { return mux.senderSchemes.Schemes() }

// ValidSenderScheme returns true iff scheme has been registered for MailProviders.
func (mux *URLMux) ValidSenderScheme(scheme string) bool {
	return mux.senderSchemes.ValidScheme(scheme)
}

// RegisterSender registers the opener with the given scheme. If an opener
// already exists for the scheme, RegisterSender panics.
func (mux *URLMux) RegisterSender(scheme string, opener SenderURLOpener) {
	mux.senderSchemes.Register("emails", "Sender", scheme, opener)
}

// OpenSender calls OpenSenderURL with the URL parsed from urlstr.
// OpenSender is safe to call from multiple goroutines.
func (mux *URLMux) OpenSender(ctx context.Context, urlstr string) (*Sender, error) {
	opener, u, err := mux.senderSchemes.FromString("Sender", urlstr)
	if err != nil {
		return nil, err
	}
	return opener.(SenderURLOpener).OpenSenderURL(ctx, u)
}

// TemplatedSenderSchemes returns a sorted slice of the registered TemplatedSender schemes.
func (mux *URLMux) TemplatedSenderSchemes() []string { return mux.templatedSenderSchemes.Schemes() }

// ValidTemplatedSenderScheme returns true iff scheme has been registered for MailProviders.
func (mux *URLMux) ValidTemplatedSenderScheme(scheme string) bool {
	return mux.templatedSenderSchemes.ValidScheme(scheme)
}

// RegisterTemplatedSender registers the opener with the given scheme. If an opener
// already exists for the scheme, RegisterTemplatedSender panics.
func (mux *URLMux) RegisterTemplatedSender(scheme string, opener TemplatedSenderURLOpener) {
	mux.templatedSenderSchemes.Register("emails", "TemplatedSender", scheme, opener)
}

// OpenTemplatedSender calls OpenTemplateSenderURL with the URL parsed from urlstr.
// OpenTemplatedSender is safe to call from multiple goroutines.
func (mux *URLMux) OpenTemplatedSender(ctx context.Context, urlstr string) (*TemplatedSender, error) {
	opener, u, err := mux.templatedSenderSchemes.FromString("TemplatedSender", urlstr)
	if err != nil {
		return nil, err
	}
	return opener.(TemplatedSenderURLOpener).OpenTemplatedSenderURL(ctx, u)
}

var defaultURLMux = new(URLMux)

// DefaultURLMux returns the URLMux used by OpenSender.
//
// Driver packages can use this to register their SenderURLOpener on the mux.
func DefaultURLMux() *URLMux {
	return defaultURLMux
}

// OpenSender opens the Sender identified by the URL given.
// See the SenderURLOpener documentation for more details.
func OpenSender(ctx context.Context, urlstr string) (*Sender, error) {
	return defaultURLMux.OpenSender(ctx, urlstr)
}

// OpenTemplatedSender opens the TemplatedSender identified by the URL given.
// See the TemplatedSenderURLOpener documentation for more details.
func OpenTemplatedSender(ctx context.Context, urlstr string) (*TemplatedSender, error) {
	return defaultURLMux.OpenTemplatedSender(ctx, urlstr)
}
