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
	"fmt"
	"io"
	"mime"
	"strings"
	"sync"
	"unicode/utf8"
)

var _addressParserPool = sync.Pool{}

func getAddressParser(s string, decoder *mime.WordDecoder) *addrParser {
	p, ok := _addressParserPool.Get().(*addrParser)
	if !ok {
		return &addrParser{s: s, dec: decoder}
	}
	p.s = s
	p.dec = decoder
	return p
}

func putAddressParser(p *addrParser) {
	p.s = ""
	p.dec = nil
	_addressParserPool.Put(p)
}

var _addressListPool = sync.Pool{}

func getAddressList() *[]Address {
	l, ok := _addressListPool.Get().(*[]Address)
	if !ok {
		addr := make([]Address, 0, 64)
		return &addr
	}
	return l
}

func putAddressList(l *[]Address) {
	*l = (*l)[:0]
	_addressListPool.Put(l)
}

// Address represents a single mail address with an optional name.
// It is used to parse the input address.
// An address is in the form of '"John Doe" <john@doe.com>' is represented
// ass Address{Name: "John Doe", Address: "john@doe.com"}.
type Address struct {
	// Name is an optional name of the address i.e. "John Doe".
	Name string
	// Address is the proper email address.
	Address string
}

// IsZero checks if the address has zero value.
func (a Address) IsZero() bool {
	return a.Name == "" && a.Address == ""
}

func (a Address) String() string {
	// Format address local@domain
	at := strings.LastIndex(a.Address, "@")
	var local, domain string
	if at < 0 {
		// This is a malformed address ("@" is required in addr-spec);
		// treat the whole address as local-part.
		local = a.Address
	} else {
		local, domain = a.Address[:at], a.Address[at+1:]
	}

	// Add quotes if needed
	quoteLocal := false
	for i, r := range local {
		if isAtext(r, false, false) {
			continue
		}
		if r == '.' {
			// Dots are okay if they are surrounded by atext.
			// We only need to check that the previous byte is
			// not a dot, and this isn't the end of the string.
			if i > 0 && local[i-1] != '.' && i < len(local)-1 {
				continue
			}
		}
		quoteLocal = true
		break
	}
	if quoteLocal {
		local = quoteString(local)

	}

	s := "<" + local + "@" + domain + ">"

	if a.Name == "" {
		return s
	}

	// If every character is printable ASCII, quoting is simple.
	allPrintable := true
	for _, r := range a.Name {
		// isWSP here should actually be isFWS,
		// but we don't support folding yet.
		if !isVchar(r) && !isWSP(r) || isMultibyte(r) {
			allPrintable = false
			break
		}
	}
	if allPrintable {
		return quoteString(a.Name) + " " + s
	}

	// Text in an encoded-word in a display-name must not contain certain
	// characters like quotes or parentheses (see RFC 2047 section 5.3).
	// When this is the case encode the name using base64 encoding.
	if strings.ContainsAny(a.Name, "\"#$%&'(),.:;<>@[]^`{|}~") {
		return mime.BEncoding.Encode("utf-8", a.Name) + " " + s
	}
	return mime.QEncoding.Encode("utf-8", a.Name) + " " + s
}

// An AddressParser is an RFC 5322 address parser.
type AddressParser struct {
	WordDecoder *mime.WordDecoder
}

// Parse parses a single RFC 5322 address of the
// form "Gogh Fir <gf@example.com>" or "foo@example.com".
func (p AddressParser) Parse(address string) (Address, error) {
	ap := getAddressParser(address, p.WordDecoder)
	defer putAddressParser(ap)

	return ap.parseSingleAddress()
}

// VerifyFormat verifies that the given string is a valid RFC 5322 address.
func (p AddressParser) VerifyFormat(address string) error {
	ap := getAddressParser(address, p.WordDecoder)
	defer putAddressParser(ap)

	return ap.verifySingleAddress()
}

// ParseAddress parses the given string as an address.
func ParseAddress(address string) (Address, error) {
	return AddressParser{}.Parse(address)
}

func VerifyAddressFormat(address string) error {
	return AddressParser{}.VerifyFormat(address)
}

// ParseList parses the given string as a list of addresses.
func (p AddressParser) ParseList(addressList string) ([]Address, error) {
	ap := getAddressParser(addressList, p.WordDecoder)
	defer putAddressParser(ap)

	return ap.parseAddressList()
}

// VerifyListFormat verifies that the given string is a valid list of RFC 5322 addresses.
func (p AddressParser) VerifyListFormat(addressList string) error {
	ap := getAddressParser(addressList, p.WordDecoder)
	defer putAddressParser(ap)

	return ap.verifyAddressList()
}

// ParseAddressList parses the given string as a list of addresses.
func ParseAddressList(addressList string) ([]Address, error) {
	return AddressParser{}.ParseList(addressList)
}

// VerifyAddressListFormat verifies that the given string is a valid list of RFC 5322 addresses.
func VerifyAddressListFormat(addressList string) error {
	return AddressParser{}.VerifyListFormat(addressList)
}

type addrParser struct {
	s   string
	dec *mime.WordDecoder // may be nil
}

func (p *addrParser) parseAddressList() ([]Address, error) {
	var list []Address
	for {
		p.skipSpace()

		// allow skipping empty entries (RFC5322 obs-addr-list)
		if p.consume(',') {
			continue
		}

		_, err := p.parseAddress(true, &list)
		if err != nil {
			return nil, err
		}
		if !p.skipCFWS() {
			return nil, errors.New("email: misformatted parenthetical comment")
		}
		if p.empty() {
			break
		}
		if p.peek() != ',' {
			return nil, errors.New("email: expected comma")
		}

		// Skip empty entries for obs-addr-list.
		for p.consume(',') {
			p.skipSpace()
		}
		if p.empty() {
			break
		}
	}
	return list, nil
}

func (p *addrParser) verifyAddressList() error {
	for {
		p.skipSpace()

		// allow skipping empty entries (RFC5322 obs-addr-list)
		if p.consume(',') {
			continue
		}

		_, err := p.parseAddress(true, nil)
		if err != nil {
			return err
		}
		if !p.skipCFWS() {
			return errors.New("email: misformatted parenthetical comment")
		}
		if p.empty() {
			break
		}
		if p.peek() != ',' {
			return errors.New("email: expected comma")
		}

		// Skip empty entries for obs-addr-list.
		for p.consume(',') {
			p.skipSpace()
		}
		if p.empty() {
			break
		}
	}
	return nil
}

func (p *addrParser) parseSingleAddress() (Address, error) {
	addrs := getAddressList()
	defer putAddressList(addrs)

	if _, err := p.parseAddress(true, addrs); err != nil {
		return Address{}, err
	}
	if !p.skipCFWS() {
		return Address{}, errors.New("email: misformatted parenthetical comment")
	}
	if !p.empty() {
		return Address{}, fmt.Errorf("email: expected single address, got %q", p.s)
	}
	if len(*addrs) == 0 {
		return Address{}, errors.New("email: empty group")
	}
	if len(*addrs) > 1 {
		return Address{}, errors.New("email: group with multiple addresses")
	}
	return (*addrs)[0], nil
}

const (
	// RFC 3696 ( https://tools.ietf.org/html/rfc3696#section-3 )
	// The domain part (after the "@") must not exceed 255 characters
	maxEmailDomainLength = 255
	// The "local part" (before the "@") must not exceed 64 characters
	maxEmailLocalLength = 64
	// Max email length must not exceed 320 characters.
	maxEmailLength = maxEmailDomainLength + maxEmailLocalLength + 1
)

func (p *addrParser) verifySingleAddress() error {
	total, err := p.parseAddress(true, nil)
	if err != nil {
		return err
	}

	if !p.skipCFWS() {
		return errors.New("email: misformatted parenthetical comment")
	}
	if !p.empty() {
		return fmt.Errorf("email: expected single address, got %q", p.s)
	}

	if total == 0 {
		return errors.New("email: empty group")
	}

	if total > 1 {
		return errors.New("email: group with multiple addresses")
	}
	return nil
}

// parseAddress parses a single RFC 5322 address at the start of p.
func (p *addrParser) parseAddress(handleGroup bool, list *[]Address) (int, error) {
	p.skipSpace()
	if p.empty() {
		return 0, errors.New("email: no address")
	}

	// address = mailbox / group
	// mailbox = name-addr / addr-spec
	// group = display-name ":" [group-list] ";" [CFWS]

	// addr-spec has a more restricted grammar than name-addr,
	// so try parsing it first, and fallback to name-addr.
	// TODO(dsymonds): Is this really correct?
	spec, err := p.consumeAddrSpec()
	if err == nil {
		var displayName string
		p.skipSpace()
		if !p.empty() && p.peek() == '(' {
			displayName, err = p.consumeDisplayNameComment()
			if err != nil {
				return 0, err
			}
		}

		// Check if we want to parse or verify the address.
		if list != nil {
			*list = append(*list, Address{
				Name:    displayName,
				Address: spec,
			})
		}
		return 1, err
	}

	// display-name
	var displayName string
	if p.peek() != '<' {
		displayName, err = p.consumePhrase()
		if err != nil {
			return 0, err
		}
	}

	p.skipSpace()
	if handleGroup {
		if p.consume(':') {
			return p.consumeGroupList(list)
		}
	}
	// angle-addr = "<" addr-spec ">"
	if !p.consume('<') {
		atext := true
		for _, r := range displayName {
			if !isAtext(r, true, false) {
				atext = false
				break
			}
		}
		if atext {
			// The input is like "foo.bar"; it's possible the input
			// meant to be "foo.bar@domain", or "foo.bar <...>".
			return 0, errors.New("email: missing '@' or angle-addr")
		}
		// The input is like "Full Name", which couldn't possibly be a
		// valid email address if followed by "@domain"; the input
		// likely meant to be "Full Name <...>".
		return 0, errors.New("email: no angle-addr")
	}
	spec, err = p.consumeAddrSpec()
	if err != nil {
		return 0, err
	}
	if !p.consume('>') {
		return 0, errors.New("email: unclosed angle-addr")
	}
	if list != nil {
		*list = append(*list, Address{
			Name:    displayName,
			Address: spec,
		})
	}
	return 1, nil
}

func (p *addrParser) consumeGroupList(group *[]Address) (int, error) {
	// handle empty group.
	p.skipSpace()
	if p.consume(';') {
		p.skipCFWS()
		return 0, nil
	}

	var total int
	for {
		p.skipSpace()
		// embedded groups not allowed.
		n, err := p.parseAddress(false, group)
		if err != nil {
			return 0, err
		}
		total += n

		if !p.skipCFWS() {
			return 0, errors.New("email: misformatted parenthetical comment")
		}
		if p.consume(';') {
			p.skipCFWS()
			break
		}
		if !p.consume(',') {
			return 0, errors.New("email: expected comma")
		}
	}
	return total, nil
}

// consumeAddrSpec parses a single RFC 5322 addr-spec at the start of p.
func (p *addrParser) consumeAddrSpec() (spec string, err error) {
	orig := *p
	defer func() {
		if err != nil {
			*p = orig
		}
	}()

	// local-part = dot-atom / quoted-string
	var localPart string
	p.skipSpace()
	if p.empty() {
		return "", errors.New("email: no addr-spec")
	}
	if p.peek() == '"' {
		// quoted-string
		localPart, err = p.consumeQuotedString()
		if localPart == "" {
			err = errors.New("email: empty quoted string in addr-spec")
		}
	} else {
		// dot-atom
		localPart, err = p.consumeAtom(true, false)
	}
	if err != nil {
		return "", err
	}

	if !p.consume('@') {
		return "", errors.New("email: missing @ in addr-spec")
	}

	// domain = dot-atom / domain-literal
	var domain string
	p.skipSpace()
	if p.empty() {
		return "", errors.New("email: no domain in addr-spec")
	}
	// TODO(dsymonds): Handle domain-literal
	domain, err = p.consumeAtom(true, false)
	if err != nil {
		return "", err
	}

	if len(domain) > maxEmailDomainLength {
		return "", fmt.Errorf("email: invalid length. Domain length should not exceed %d characters", maxEmailDomainLength)
	}

	if len(localPart) > maxEmailLocalLength {
		return "", fmt.Errorf("email: invalid length. Local part length should not exceed %d characters", maxEmailLocalLength)
	}

	address := localPart + "@" + domain
	if len(address) > maxEmailLength {
		return "", fmt.Errorf("email: invalid length. Total length should not exceed %d characters", maxEmailLength)
	}

	return address, nil
}

// consumePhrase parses the RFC 5322 phrase at the start of p.
func (p *addrParser) consumePhrase() (phrase string, err error) {
	// phrase = 1*word
	var isPrevEncoded bool

	var wordCount int
	var sb strings.Builder
	for {
		// word = atom / quoted-string
		var word string
		p.skipSpace()
		if p.empty() {
			break
		}
		isEncoded := false
		if p.peek() == '"' {
			// quoted-string
			word, err = p.consumeQuotedString()
		} else {
			// atom
			// We actually parse dot-atom here to be more permissive
			// than what RFC 5322 specifies.
			word, err = p.consumeAtom(true, true)
			if err == nil {
				word, isEncoded, err = p.decodeRFC2047Word(word)
			}
		}

		if err != nil {
			break
		}

		if isPrevEncoded && isEncoded {
			// words[len(words)-1] += word
			sb.WriteString(word)
		} else {
			if wordCount != 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(word)
			wordCount++
		}
		isPrevEncoded = isEncoded
	}
	// Ignore any error if we got at least one word.
	if err != nil && wordCount == 0 {
		return "", fmt.Errorf("email: missing word in phrase: %v", err)
	}

	return sb.String(), nil
}

// consumeQuotedString parses the quoted string at the start of p.
func (p *addrParser) consumeQuotedString() (qs string, err error) {
	// Assume first byte is '"'.
	i := 1
	var sb strings.Builder

	escaped := false

Loop:
	for {
		r, size := utf8.DecodeRuneInString(p.s[i:])

		switch {
		case size == 0:
			return "", errors.New("email: unclosed quoted-string")

		case size == 1 && r == utf8.RuneError:
			return "", fmt.Errorf("email: invalid utf-8 in quoted-string: %q", p.s)

		case escaped:
			//  quoted-pair = ("\" (VCHAR / WSP))

			if !isVchar(r) && !isWSP(r) {
				return "", fmt.Errorf("email: bad character in quoted-string: %q", r)
			}

			sb.WriteRune(r)
			escaped = false

		case isQtext(r) || isWSP(r):
			// qtext (printable US-ASCII excluding " and \), or
			// FWS (almost; we're ignoring CRLF)
			sb.WriteRune(r)
		case r == '"':
			break Loop
		case r == '\\':
			escaped = true
		default:
			return "", fmt.Errorf("email: bad character in quoted-string: %q", r)
		}
		i += size
	}
	p.s = p.s[i+1:]
	return sb.String(), nil
}

// consumeAtom parses an RFC 5322 atom at the start of p.
// If dot is true, consumeAtom parses an RFC 5322 dot-atom instead.
// If permissive is true, consumeAtom will not fail on:
// - leading/trailing/double dots in the atom (see golang.org/issue/4938)
// - special characters (RFC 5322 3.2.3) except '<', '>', ':' and '"' (see golang.org/issue/21018)
func (p *addrParser) consumeAtom(dot bool, permissive bool) (atom string, err error) {
	i := 0

Loop:
	for {
		r, size := utf8.DecodeRuneInString(p.s[i:])
		switch {
		case size == 1 && r == utf8.RuneError:
			return "", fmt.Errorf("email: invalid utf-8 in address: %q", p.s)

		case size == 0 || !isAtext(r, dot, permissive):
			break Loop

		default:
			i += size

		}
	}

	if i == 0 {
		return "", errors.New("email: invalid string")
	}
	atom, p.s = p.s[:i], p.s[i:]
	if !permissive {
		if strings.HasPrefix(atom, ".") {
			return "", errors.New("email: leading dot in atom")
		}
		if strings.Contains(atom, "..") {
			return "", errors.New("email: double dot in atom")
		}
		if strings.HasSuffix(atom, ".") {
			return "", errors.New("email: trailing dot in atom")
		}
	}
	return atom, nil
}

func (p *addrParser) consumeDisplayNameComment() (string, error) {
	if !p.consume('(') {
		return "", errors.New("email: comment does not start with (")
	}
	comment, ok := p.consumeComment()
	if !ok {
		return "", errors.New("email: misformatted parenthetical comment")
	}

	// TODO(stapelberg): parse quoted-string within comment
	words := strings.FieldsFunc(comment, func(r rune) bool { return r == ' ' || r == '\t' })
	for idx, word := range words {
		decoded, isEncoded, err := p.decodeRFC2047Word(word)
		if err != nil {
			return "", err
		}
		if isEncoded {
			words[idx] = decoded
		}
	}

	return strings.Join(words, " "), nil
}

func (p *addrParser) consume(c byte) bool {
	if p.empty() || p.peek() != c {
		return false
	}
	p.s = p.s[1:]
	return true
}

// skipSpace skips the leading space and tab characters.
func (p *addrParser) skipSpace() {
	p.s = strings.TrimLeft(p.s, " \t")
}

func (p *addrParser) peek() byte {
	return p.s[0]
}

func (p *addrParser) empty() bool {
	return p.len() == 0
}

func (p *addrParser) len() int {
	return len(p.s)
}

// skipCFWS skips CFWS as defined in RFC5322.
func (p *addrParser) skipCFWS() bool {
	p.skipSpace()

	for {
		if !p.consume('(') {
			break
		}

		if _, ok := p.consumeComment(); !ok {
			return false
		}

		p.skipSpace()
	}

	return true
}

func (p *addrParser) consumeComment() (string, bool) {
	// '(' already consumed.
	depth := 1

	var comment string
	for {
		if p.empty() || depth == 0 {
			break
		}

		if p.peek() == '\\' && p.len() > 1 {
			p.s = p.s[1:]
		} else if p.peek() == '(' {
			depth++
		} else if p.peek() == ')' {
			depth--
		}
		if depth > 0 {
			comment += p.s[:1]
		}
		p.s = p.s[1:]
	}

	return comment, depth == 0
}

func (p *addrParser) decodeRFC2047Word(s string) (word string, isEncoded bool, err error) {
	dec := p.dec
	if dec == nil {
		dec = &rfc2047Decoder
	}

	// Substitute our own CharsetReader function so that we can tell
	// whether an error from the Decode method was due to the
	// CharsetReader (meaning the charset is invalid).
	// We used to look for the charsetError type in the error result,
	// but that behaves badly with CharsetReaders other than the
	// one in rfc2047Decoder.
	adec := *dec
	charsetReaderError := false
	adec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		if dec.CharsetReader == nil {
			charsetReaderError = true
			return nil, charsetError(charset)
		}
		r, err := dec.CharsetReader(charset, input)
		if err != nil {
			charsetReaderError = true
		}
		return r, err
	}
	word, err = adec.Decode(s)
	if err == nil {
		return word, true, nil
	}

	// If the error came from the character set reader
	// (meaning the character set itself is invalid
	// but the decoding worked fine until then),
	// return the original text and the error,
	// with isEncoded=true.
	if charsetReaderError {
		return s, true, err
	}

	// Ignore invalid RFC 2047 encoded-word errors.
	return s, false, nil
}

var rfc2047Decoder = mime.WordDecoder{
	CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
		return nil, charsetError(charset)
	},
}

type charsetError string

func (e charsetError) Error() string {
	return fmt.Sprintf("charset not supported: %q", string(e))
}

// isAtext reports whether r is an RFC 5322 atext character.
// If dot is true, period is included.
// If permissive is true, RFC 5322 3.2.3 specials is included,
// except '<', '>', ':' and '"'.
func isAtext(r rune, dot, permissive bool) bool {
	switch r {
	case '.':
		return dot

	// RFC 5322 3.2.3. specials
	case '(', ')', '[', ']', ';', '@', '\\', ',':
		return permissive

	case '<', '>', '"', ':':
		return false
	}
	return isVchar(r)
}

// isQtext reports whether r is an RFC 5322 qtext character.
func isQtext(r rune) bool {
	// Printable US-ASCII, excluding backslash or quote.
	if r == '\\' || r == '"' {
		return false
	}
	return isVchar(r)
}

// quoteString renders a string as an RFC 5322 quoted-string.
func quoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if isQtext(r) || isWSP(r) {
			b.WriteRune(r)
		} else if isVchar(r) {
			b.WriteByte('\\')
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// isVchar reports whether r is an RFC 5322 VCHAR character.
func isVchar(r rune) bool {
	// Visible (printing) characters.
	return '!' <= r && r <= '~' || isMultibyte(r)
}

// isMultibyte reports whether r is a multi-byte UTF-8 character
// as supported by RFC 6532.
func isMultibyte(r rune) bool {
	return r >= utf8.RuneSelf
}

// isWSP reports whether r is a WSP (white space).
// WSP is a space or horizontal tab (RFC 5234 Appendix B).
func isWSP(r rune) bool {
	return r == ' ' || r == '\t'
}
