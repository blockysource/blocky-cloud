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
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ParseDialerURL parses Dialer from the input URL.
// The URL is expected to be in the following format:
//
//	smtp[s]://[username[:password]@]host[:port][?localname=localname&ssl=ssl&auth=auth]
//
// The username and password are used to authenticate to the smtp server.
// The following query parameters are supported:
//   - localname: the domain of the sender, which is used for the HELO/EHLO, if not defined, the Domain field is used.
//     by default, "localhost" is sent.
//   - ssl: defines whether an SSL connection is used. It should be false in most cases since the authentication mechanism should use the STARTTLS extension instead.
//   - auth: the authentication mechanism used when the smtp server does not support the STARTTLS extension.
//     The following values are supported: "plain", "login", "md5".
//     By default client tries to check if server supports CRAM-MD5, LOGIN, PLAIN authentication in this order.
func ParseDialerURL(smtpURL string) (*Dialer, error) {
	u, err := url.Parse(smtpURL)
	if err != nil {
		return nil, fmt.Errorf("smtpemail: failed to parse BLOCKY_SMTP_URL: %w", err)
	}
	// Parse the username and password.
	d := Dialer{LocalName: "localhost"}
	username := u.User.Username()
	if username == "" {
		d.Username = username
	}
	password, _ := u.User.Password()
	if password == "" {
		d.Password = password
	}

	// Parse the host and port.
	host, portS, _ := net.SplitHostPort(u.Host)
	if host != "" {
		d.Host = host
	}
	if portS != "" {
		port, err := strconv.ParseUint(portS, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("smtpemail: failed to parse port: %w", err)
		}
		d.Port = uint16(port)
	}

	var auth string
	for k, v := range u.Query() {
		switch strings.ToLower(k) {
		case "localname":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid localname: %s", v)
			}
			d.LocalName = v[0]
		case "ssl":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid ssl: %s", v)
			}
			vb := v[0]
			if len(vb) == 0 {
				d.SSL = true
			} else {
				ssl, err := strconv.ParseBool(vb)
				if err != nil {
					return nil, fmt.Errorf("smtpemail: invalid ssl: %s", v)
				}
				d.SSL = ssl
			}
		case "auth":
			if len(v) != 1 {
				return nil, fmt.Errorf("smtpemail: invalid auth: %s", v)
			}
			auth = v[0]

			switch strings.ToLower(auth) {
			case "plain", "login", "md5":
			default:
				return nil, fmt.Errorf("smtpemail: invalid auth: %s", v)
			}
		default:
			return nil, fmt.Errorf("smtpemail: invalid query parameter: %s", k)
		}
	}

	if auth != "" {
		var err error
		d.Auth, err = smtpAuth(d.Username, d.Password, d.Host, auth)
		if err != nil {
			return nil, err
		}
	}

	if err = d.Validate(); err != nil {
		return nil, err
	}

	return &d, nil
}

// Dialer is used to dial the smtp server.
type Dialer struct {
	// Host is the hostname or IP address of the smtp server.
	Host string
	// Port is the Port of the smtp server.
	Port uint16
	// Username is the Username used to authenticate to the smtp server.
	Username string
	// Password is the Password used to authenticate to the smtp server.
	Password string
	// LocalName is the hostname of the sender machine, which is used for the HELO/EHLO.
	// By default, "localhost" is sent.
	// Some SMTP servers requires LocalName to match the ip address of the sender machine.
	LocalName string
	// Auth is the authentication mechanism used when the smtp server does not
	Auth smtp.Auth
	// SSL defines whether an SSL connection is used. It should be false in
	// most cases since the authentication mechanism should use the STARTTLS
	// extension instead.
	SSL bool
	// TSLConfig represents the TLS configuration used for the TLS (when the
	// STARTTLS extension is used) or SSL connection.
	TLSConfig *tls.Config
}

// Validate validates the Dialer configuration.
func (d *Dialer) Validate() error {
	if d.Host == "" {
		return errors.New("smtpemail: missing host")
	}

	if d.Port == 0 {
		return errors.New("smtpemail: missing port")
	}

	return nil
}

func (d *Dialer) dial(ctx context.Context) (*smtp.Client, error) {
	dl := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dl.DialContext(ctx, "tcp", d.addr())
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("smtpemail: failed to dial smtp server: %w", err)
	}

	if d.SSL {
		conn = tls.Client(conn, d.tlsConfig())
	}

	c, err := smtp.NewClient(conn, d.Host)
	if err != nil {
		return nil, err
	}

	if d.LocalName != "" {
		if err := c.Hello(d.LocalName); err != nil {
			return nil, err
		}
	}

	if !d.SSL {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(d.tlsConfig()); err != nil {
				c.Close()
				return nil, err
			}
		}
	}

	auth := d.Auth
	if d.Auth == nil && d.Username != "" {
		if ok, auths := c.Extension("AUTH"); ok {
			if strings.Contains(auths, "CRAM-MD5") {
				auth = smtp.CRAMMD5Auth(d.Username, d.Password)
			} else if strings.Contains(auths, "LOGIN") && !strings.Contains(auths, "PLAIN") {
				auth = &loginAuth{
					username: d.Username,
					password: d.Password,
					host:     d.Host,
				}
			} else {
				auth = smtp.PlainAuth("", d.Username, d.Password, d.Host)
			}
		}
	}

	if auth != nil {
		if err = c.Auth(auth); err != nil {
			c.Close()
			return nil, err
		}
	}

	return nil, nil
}

func (d *Dialer) addr() string {
	return fmt.Sprintf("%s:%d", d.Host, d.Port)
}

func (d *Dialer) tlsConfig() *tls.Config {
	if d.TLSConfig == nil {
		return &tls.Config{ServerName: d.Host}
	}
	return d.TLSConfig
}
