package specindex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Index struct {
	GeneratedAt string       `json:"generated_at,omitempty"`
	Source      string       `json:"source,omitempty"`
	Tags        []TagInfo    `json:"tags"`
	Operations  []*Operation `json:"operations"`

	aliasOnce sync.Once             `json:"-"`
	aliasMap  map[string]*Operation `json:"-"`
}

type TagInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Operation struct {
	OperationID string       `json:"operation_id"`
	Method      string       `json:"method"`
	Path        string       `json:"path"`
	Tags        []string     `json:"tags"`
	Summary     string       `json:"summary,omitempty"`
	Description string       `json:"description,omitempty"`
	Parameters  []Parameter  `json:"parameters,omitempty"`
	RequestBody *RequestBody `json:"request_body,omitempty"`
	Responses   []Response   `json:"responses,omitempty"`
	Aliases     []string     `json:"aliases,omitempty"`
}

type Parameter struct {
	Name        string        `json:"name"`
	In          string        `json:"in"`
	Required    bool          `json:"required"`
	Description string        `json:"description,omitempty"`
	Schema      *SchemaDigest `json:"schema,omitempty"`
	Default     any           `json:"default,omitempty"`
}

type RequestBody struct {
	Required    bool          `json:"required"`
	ContentType string        `json:"content_type"`
	Schema      *SchemaDigest `json:"schema,omitempty"`
	Fields      []Parameter   `json:"fields,omitempty"`
}

type Response struct {
	Status       string        `json:"status"`
	ContentTypes []string      `json:"content_types,omitempty"`
	Binary       bool          `json:"binary"`
	Schema       *SchemaDigest `json:"schema,omitempty"`
	Description  string        `json:"description,omitempty"`
}

type SchemaProperty struct {
	Name     string        `json:"name"`
	Required bool          `json:"required"`
	Schema   *SchemaDigest `json:"schema,omitempty"`
}

type SchemaDigest struct {
	Type        string           `json:"type,omitempty"`
	Format      string           `json:"format,omitempty"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Enum        []string         `json:"enum,omitempty"`
	Items       *SchemaDigest    `json:"items,omitempty"`
	Properties  []SchemaProperty `json:"properties,omitempty"`
	OneOf       []*SchemaDigest  `json:"one_of,omitempty"`
	AnyOf       []*SchemaDigest  `json:"any_of,omitempty"`
	AllOf       []*SchemaDigest  `json:"all_of,omitempty"`
}

func (idx *Index) Clone() *Index {
	if idx == nil {
		return nil
	}
	raw, _ := json.Marshal(idx)
	var out Index
	_ = json.Unmarshal(raw, &out)
	return &out
}

func (idx *Index) ResolveOperation(name string) (*Operation, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, fmt.Errorf("operation is required")
	}
	idx.aliasOnce.Do(func() {
		idx.aliasMap = make(map[string]*Operation, len(idx.Operations)*4)
		for _, op := range idx.Operations {
			if op == nil {
				continue
			}
			register := func(key string) {
				normalized := strings.ToLower(strings.TrimSpace(key))
				if normalized == "" {
					return
				}
				if _, exists := idx.aliasMap[normalized]; !exists {
					idx.aliasMap[normalized] = op
				}
			}
			register(op.OperationID)
			register(strings.ReplaceAll(op.OperationID, "_", "-"))
			register(strings.ReplaceAll(op.OperationID, "-", "_"))
			for _, alias := range op.Aliases {
				register(alias)
			}
		}
	})
	if op, ok := idx.aliasMap[strings.ToLower(trimmed)]; ok {
		return op, nil
	}
	return nil, fmt.Errorf("unknown operation %q", name)
}

func (idx *Index) Search(query, tag, method string, limit int) []*Operation {
	if idx == nil {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	query = strings.ToLower(strings.TrimSpace(query))
	tag = strings.ToLower(strings.TrimSpace(tag))
	method = strings.ToUpper(strings.TrimSpace(method))
	type scored struct {
		score int
		op    *Operation
	}
	var items []scored
	for _, op := range idx.Operations {
		if op == nil {
			continue
		}
		if method != "" && op.Method != method {
			continue
		}
		if tag != "" {
			matched := false
			for _, candidate := range op.Tags {
				if strings.EqualFold(candidate, tag) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if query == "" {
			items = append(items, scored{score: 1, op: op})
			continue
		}
		score := scoreOperation(op, query)
		if score > 0 {
			items = append(items, scored{score: score, op: op})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			if items[i].op.Method == items[j].op.Method {
				return items[i].op.OperationID < items[j].op.OperationID
			}
			return items[i].op.Method < items[j].op.Method
		}
		return items[i].score > items[j].score
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]*Operation, 0, len(items))
	for _, item := range items {
		out = append(out, item.op)
	}
	return out
}

func scoreOperation(op *Operation, query string) int {
	score := 0
	add := func(value string, exact, contains int) {
		normalized := strings.ToLower(value)
		switch {
		case normalized == query:
			score += exact
		case strings.Contains(normalized, query):
			score += contains
		}
	}
	add(op.OperationID, 100, 60)
	add(op.Path, 80, 45)
	add(op.Method+":"+strings.TrimPrefix(op.Path, "/api/v1/"), 90, 50)
	add(op.Summary, 40, 20)
	add(op.Description, 25, 12)
	for _, tag := range op.Tags {
		add(tag, 35, 15)
	}
	for _, alias := range op.Aliases {
		add(alias, 70, 30)
	}
	return score
}

func (op *Operation) ParamsByLocation(location string) []Parameter {
	out := make([]Parameter, 0)
	for _, parameter := range op.Parameters {
		if parameter.In == location {
			out = append(out, parameter)
		}
	}
	return out
}

func (op *Operation) AllLocationsForName(name string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for _, parameter := range op.Parameters {
		if parameter.Name == name {
			if _, ok := seen[parameter.In]; !ok {
				out = append(out, parameter.In)
				seen[parameter.In] = struct{}{}
			}
		}
	}
	if op.RequestBody != nil {
		for _, field := range op.RequestBody.Fields {
			if field.Name == name {
				if _, ok := seen["body"]; !ok {
					out = append(out, "body")
					seen["body"] = struct{}{}
				}
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func (rb *RequestBody) Kind() string {
	switch rb.ContentType {
	case "multipart/form-data":
		return "multipart"
	case "application/x-www-form-urlencoded":
		return "form"
	case "application/json":
		return "json"
	case "text/plain":
		return "text"
	default:
		return "raw"
	}
}

func (rb *RequestBody) FieldByName(name string) *Parameter {
	for i := range rb.Fields {
		if rb.Fields[i].Name == name {
			return &rb.Fields[i]
		}
	}
	return nil
}

func (parameter Parameter) IsBinary() bool {
	return parameter.Schema != nil && parameter.Schema.Type == "string" && parameter.Schema.Format == "binary"
}

func (schema *SchemaDigest) Summary() string {
	if schema == nil {
		return "any"
	}
	if len(schema.Enum) > 0 {
		values := schema.Enum
		if len(values) > 5 {
			values = append([]string{}, values[:5]...)
			values = append(values, "...")
		}
		return "enum[" + strings.Join(values, ", ") + "]"
	}
	if len(schema.OneOf) > 0 {
		parts := make([]string, 0, len(schema.OneOf))
		for _, item := range schema.OneOf {
			parts = append(parts, item.Summary())
		}
		return "oneOf(" + strings.Join(parts, ", ") + ")"
	}
	if len(schema.AnyOf) > 0 {
		parts := make([]string, 0, len(schema.AnyOf))
		for _, item := range schema.AnyOf {
			parts = append(parts, item.Summary())
		}
		return "anyOf(" + strings.Join(parts, ", ") + ")"
	}
	if len(schema.AllOf) > 0 {
		parts := make([]string, 0, len(schema.AllOf))
		for _, item := range schema.AllOf {
			parts = append(parts, item.Summary())
		}
		return "allOf(" + strings.Join(parts, ", ") + ")"
	}
	if schema.Type == "array" && schema.Items != nil {
		return "array<" + schema.Items.Summary() + ">"
	}
	if schema.Type == "object" {
		return "object"
	}
	if schema.Type == "" {
		return "any"
	}
	if schema.Format != "" {
		return schema.Type + ":" + schema.Format
	}
	return schema.Type
}
