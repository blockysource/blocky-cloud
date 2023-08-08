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
	"bytes"
	"encoding/gob"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"github.com/blockysource/blocky-cloud/email/driver"
)

func init() {
	gob.Register(map[string]any{})
}

// ParsedTemplates is a struct that represents a parsed email templates.
type ParsedTemplates struct {
	mu sync.RWMutex

	templates map[string]*ParsedTemplate
	data      map[string]any
}

// Execute executes the template and returns a message.
func (p *ParsedTemplates) Execute(tm *driver.TemplatedMessage) (*driver.Message, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	t, ok := p.findVersion(tm.TemplateName, tm.TemplateVersion)
	if !ok {
		return nil, fmt.Errorf("emailtemplate: template %q version %q not found", tm.TemplateName, tm.TemplateVersion)
	}
	return t.Execute(tm)
}

// Add adds a template to the ParsedTemplates struct.
func (p *ParsedTemplates) Add(t *Template) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.templates == nil {
		p.templates = make(map[string]*ParsedTemplate)
	}

	if _, ok := p.templates[t.Name]; ok {
		return fmt.Errorf("emailtemplate: template %q already exists", t.Name)
	}

	data := mergeData(p.data, t.DefaultData)
	versions := make([]*ParsedTemplateVersion, len(t.Versions))
	for i, v := range t.Versions {
		parsed, err := v.parse(data)
		if err != nil {
			return err
		}
		versions[i] = parsed
	}

	p.templates[t.Name] = &ParsedTemplate{
		name:     t.Name,
		versions: versions,
		data:     data,
	}

	return nil
}

// Get returns a template by name.
func (p *ParsedTemplates) Get(name string) (*ParsedTemplate, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	temp, ok := p.templates[name]
	if !ok {
		return nil, false
	}
	return temp, true
}

// GetVersion returns a template by name and version.
func (p *ParsedTemplates) GetVersion(name string, version string) (*ParsedTemplateVersion, bool) {
	return p.findVersion(name, version)
}

func (p *ParsedTemplates) findVersion(name string, version string) (*ParsedTemplateVersion, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	temp, ok := p.templates[name]
	if !ok {
		return nil, false
	}
	for _, v := range temp.versions {
		if v.version == version {
			return v, true
		}
	}
	return nil, false
}

// AddVersion adds a template version to the ParsedTemplates struct.
func (p *ParsedTemplates) AddVersion(name string, version *TemplateVersion) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.templates == nil {
		return fmt.Errorf("emailtemplate: template %q not found", name)
	}

	temp, ok := p.templates[name]
	if !ok {
		return fmt.Errorf("emailtemplate: template %q not found", name)
	}
	for _, v := range temp.versions {
		if v.version == version.Version {
			return fmt.Errorf("emailtemplate: template %q version %q already exists", name, version.Version)
		}
	}

	parsed, err := version.parse(temp.data)
	if err != nil {
		return err
	}

	temp.versions = append(temp.versions, parsed)
	p.templates[name] = temp

	return nil
}

// UpsertVersion inserts or updates a template version.
func (p *ParsedTemplates) UpsertVersion(name string, version *TemplateVersion) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.templates == nil {
		return fmt.Errorf("emailtemplate: template %q not found", name)
	}

	temp, ok := p.templates[name]
	if !ok {
		return fmt.Errorf("emailtemplate: template %q not found", name)
	}

	idx := -1
	for i, v := range temp.versions {
		if v.version == version.Version {
			idx = i
			break
		}
	}
	parsed, err := version.parse(temp.data)
	if err != nil {
		return err
	}
	if idx != -1 {
		temp.versions[idx] = parsed
	} else {
		temp.versions = append(temp.versions, parsed)
	}
	p.templates[name] = temp
	return nil
}

// ParseTemplates parses input templates and returns a ParsedTemplates struct.
func ParseTemplates(generalData map[string]any, templates ...*Template) (*ParsedTemplates, error) {
	p := &ParsedTemplates{templates: make(map[string]*ParsedTemplate), data: generalData}
	for _, t := range templates {
		if err := p.Add(t); err != nil {
			return nil, err
		}
	}
	return p, nil
}

type (
	// Template is a struct that represents an email template.
	Template struct {
		// Name is the unique template name.
		Name string

		// Versions is a list of template versions.
		Versions []TemplateVersion

		// DefaultData is a map of default template data values.
		// This value is optional. The data from general data will be used if not provided.
		DefaultData map[string]any
	}
	// TemplateVersion is a struct that represents a template version.
	TemplateVersion struct {
		// Version is the version of the template.
		Version string

		// Content is the content of the template version.
		// The content should define the following sub-templates:
		// - from_t - the from address of the email
		// - to_t - the to address of the email
		// - cc_t - the cc address of the email
		// - bcc_t - the bcc address of the email
		// - subject_t - the subject of the email
		// - html_t - the html body of the email
		// - text_t - the text body of the email
		Content string

		// DefaultData is a map of default template data values.
		// The data from template, or general data will be used if not provided.
		DefaultData map[string]any
	}
)

func (t *TemplateVersion) parse(generalData map[string]any) (*ParsedTemplateVersion, error) {
	var err error
	parsed := &ParsedTemplateVersion{version: t.Version}
	tv, err := template.New(t.Version).Parse(t.Content)
	if err != nil {
		return nil, fmt.Errorf("emailtemplate: parsing template %q version %q failed: %w", t.Version, t.Version, err)
	}
	parsed.from = tv.Lookup("from_t")
	parsed.to = tv.Lookup("to_t")
	parsed.cc = tv.Lookup("cc_t")
	parsed.bcc = tv.Lookup("bcc_t")
	parsed.subj = tv.Lookup("subject_t")
	parsed.html = tv.Lookup("html_t")
	parsed.text = tv.Lookup("text_t")

	parsed.data = mergeData(generalData, t.DefaultData)
	return parsed, nil
}

type (
	// ParsedTemplate is a struct that represents a parsed email template.
	ParsedTemplate struct {
		mu       sync.RWMutex
		name     string
		versions []*ParsedTemplateVersion

		data map[string]any
		root *ParsedTemplates
	}
	// ParsedTemplateVersion is a struct that represents a parsed email template.
	ParsedTemplateVersion struct {
		mu      sync.RWMutex
		version string
		from    *template.Template
		to      *template.Template
		cc      *template.Template
		bcc     *template.Template
		subj    *template.Template
		html    *template.Template
		text    *template.Template
		data    map[string]any
		root    *ParsedTemplate
	}
)

// ReplaceData replaces the template data.
func (p *ParsedTemplate) ReplaceData(data map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.data = mergeData(p.root.data, deepCopyMap(data))

	for _, v := range p.versions {
		v.mu.Lock()
		v.data = mergeData(p.data, v.data)
		v.mu.Unlock()
	}
}

// ReplaceData replaces the template data.
func (p *ParsedTemplateVersion) ReplaceData(data map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.data = mergeData(p.root.data, deepCopyMap(data))
}

// Execute executes the template and returns a message.
// If the template defines a to ,cc, bcc templates
// the template data provided to these templates will contain the '_index' key with the index of the recipient.
func (p *ParsedTemplateVersion) Execute(in *driver.TemplatedMessage) (*driver.Message, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	msg := driver.Message{
		MessageID: in.MessageID,
		ReplyTo:   in.ReplyTo,
		Date:      in.Date,
	}

	data := mergeData(p.data, in.TemplateData)
	from := in.From

	var sb strings.Builder

	var err error
	if from == "" && p.from != nil {
		if err = p.from.Execute(&sb, data); err != nil {
			return nil, err
		}
		from = sb.String()
		sb.Reset()
	}
	msg.From = from

	var to []string
	if len(in.To) == 0 && p.to != nil {
		if err = p.to.Execute(&sb, data); err != nil {
			return nil, err
		}
		addrString := sb.String()
		sb.Reset()
		to = append(to, addrString)
	}
	for idx, addr := range in.To {
		if addr == "" && p.to != nil {
			data["_index"] = idx
			if err = p.to.Execute(&sb, data); err != nil {
				return nil, err
			}
			delete(data, "_index")
			addr = sb.String()
			sb.Reset()
		}

		to = append(to, addr)
	}
	msg.To = to

	var cc []string
	if len(in.Cc) == 0 && p.cc != nil {
		if err = p.cc.Execute(&sb, data); err != nil {
			return nil, err
		}
		addrString := sb.String()
		sb.Reset()
		if addrString != "" {
			cc = append(cc, addrString)
		}
	}
	for idx, addr := range in.Cc {
		if p.cc != nil {
			data["_index"] = idx
			if err = p.cc.Execute(&sb, data); err != nil {
				return nil, err
			}
			delete(data, "_index")
			addr = sb.String()
			sb.Reset()
		}

		cc = append(cc, addr)
	}
	msg.Cc = cc

	var bcc []string
	if len(in.Bcc) == 0 && p.bcc != nil {
		if err = p.bcc.Execute(&sb, data); err != nil {
			return nil, err
		}
		addrString := sb.String()
		sb.Reset()
		if addrString != "" {
			bcc = append(bcc, addrString)
		}
	}
	for idx, addr := range in.Cc {
		if addr == "" && p.bcc != nil {
			data["_index"] = idx
			if err = p.bcc.Execute(&sb, data); err != nil {
				return nil, err
			}
			delete(data, "_index")
			addr = sb.String()
			sb.Reset()
		}
		bcc = append(bcc, addr)
	}
	msg.Bcc = bcc

	subj := in.Subject
	if p.subj != nil {
		if err = p.subj.Execute(&sb, data); err != nil {
			return nil, err
		}
		subj = sb.String()
		sb.Reset()
	}
	msg.Subject = subj

	html := in.HtmlBody
	if p.html != nil {
		if err = p.html.Execute(&sb, data); err != nil {
			return nil, err
		}
		html = sb.String()
		sb.Reset()
	}
	msg.HtmlBody = html

	text := in.TextBody
	if p.text != nil {
		if err = p.text.Execute(&sb, data); err != nil {
			return nil, err
		}
		text = sb.String()
		sb.Reset()
	}

	msg.TextBody = text

	return &msg, nil
}

var emptyMap = make(map[string]any)

func mergeData(base map[string]any, override map[string]any) map[string]any {
	if base == nil && override == nil {
		return emptyMap
	}
	if override == nil {
		return deepCopyMap(base)
	}
	if base == nil {
		return override
	}
	bc := deepCopyMap(base)
	out := make(map[string]any, len(base)+len(override))
	for k, v := range bc {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

var _bufferPool = sync.Pool{}

func getBuffer() *bytes.Buffer {
	buf := _bufferPool.Get()
	if buf == nil {
		return &bytes.Buffer{}
	}
	return buf.(*bytes.Buffer)
}

func putBuffer(buf *bytes.Buffer) {
	buf.Reset()
	_bufferPool.Put(buf)
}

func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}

	buf := getBuffer()
	defer putBuffer(buf)

	if err := gob.NewEncoder(buf).Encode(m); err != nil {
		panic(fmt.Errorf("emailtemplate: encoding map failed: %w", err))
	}

	var cp map[string]any
	err := gob.NewDecoder(buf).
		Decode(&cp)
	if err != nil {
		panic(fmt.Errorf("emailtemplate: decoding map failed: %w", err))
	}
	return cp
}
