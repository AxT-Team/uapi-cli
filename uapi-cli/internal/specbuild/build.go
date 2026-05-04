package specbuild

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/AxT-Team/uapi-cli/internal/specindex"
)

var contentTypePreference = []string{
	"application/json",
	"multipart/form-data",
	"application/x-www-form-urlencoded",
	"text/plain",
	"application/octet-stream",
}

const apiPrefix = "/api/v1"

func BuildFromFile(specPath string) (*specindex.Index, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("load openapi: %w", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validate openapi: %w", err)
	}

	idx := &specindex.Index{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Source:      filepath.Base(specPath),
		Tags:        buildTags(doc),
	}

	if doc.Paths == nil {
		return idx, nil
	}

	paths := make([]string, 0, doc.Paths.Len())
	for path := range doc.Paths.Map() {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		pathItem := doc.Paths.Value(path)
		if pathItem == nil {
			continue
		}
		canonicalPath := normalizeOperationPath(path)
		commonParameters, err := buildParameters(pathItem.Parameters)
		if err != nil {
			return nil, fmt.Errorf("%s common params: %w", path, err)
		}
		operations := pathItem.Operations()
		methods := make([]string, 0, len(operations))
		for method := range operations {
			methods = append(methods, method)
		}
		sort.Strings(methods)
		for _, method := range methods {
			op := operations[method]
			if op == nil {
				continue
			}
			parameters, err := buildParameters(op.Parameters)
			if err != nil {
				return nil, fmt.Errorf("%s %s params: %w", method, path, err)
			}
			requestBody, err := buildRequestBody(op.RequestBody)
			if err != nil {
				return nil, fmt.Errorf("%s %s request body: %w", method, path, err)
			}
			responses, err := buildResponses(op.Responses)
			if err != nil {
				return nil, fmt.Errorf("%s %s responses: %w", method, path, err)
			}
			operationID := strings.TrimSpace(op.OperationID)
			if operationID == "" {
				operationID = strings.ToLower(method) + "-" + strings.ReplaceAll(strings.Trim(path, "/"), "/", "-")
			}
			allParams := append([]specindex.Parameter{}, commonParameters...)
			allParams = append(allParams, parameters...)
			entry := &specindex.Operation{
				OperationID: operationID,
				Method:      strings.ToUpper(method),
				Path:        canonicalPath,
				Tags:        append([]string{}, op.Tags...),
				Summary:     strings.TrimSpace(op.Summary),
				Description: flatten(strings.TrimSpace(op.Description)),
				Parameters:  allParams,
				RequestBody: requestBody,
				Responses:   responses,
			}
			entry.Aliases = buildAliases(entry)
			idx.Operations = append(idx.Operations, entry)
		}
	}

	return idx, nil
}

func buildTags(doc *openapi3.T) []specindex.TagInfo {
	out := make([]specindex.TagInfo, 0, len(doc.Tags))
	for _, tag := range doc.Tags {
		if tag == nil {
			continue
		}
		out = append(out, specindex.TagInfo{
			Name:        tag.Name,
			Description: flatten(strings.TrimSpace(tag.Description)),
		})
	}
	return out
}

func buildParameters(parameters openapi3.Parameters) ([]specindex.Parameter, error) {
	out := make([]specindex.Parameter, 0, len(parameters))
	for _, ref := range parameters {
		if ref == nil || ref.Value == nil {
			continue
		}
		digest := schemaDigest(ref.Value.Schema)
		out = append(out, specindex.Parameter{
			Name:        ref.Value.Name,
			In:          ref.Value.In,
			Required:    ref.Value.Required,
			Description: flatten(strings.TrimSpace(ref.Value.Description)),
			Schema:      digest,
			Default:     defaultFromSchema(ref.Value.Schema),
		})
	}
	return out, nil
}

func buildRequestBody(ref *openapi3.RequestBodyRef) (*specindex.RequestBody, error) {
	if ref == nil || ref.Value == nil || len(ref.Value.Content) == 0 {
		return nil, nil
	}
	contentType, mediaType := pickContent(ref.Value.Content)
	if mediaType == nil {
		return nil, nil
	}
	out := &specindex.RequestBody{
		Required:    ref.Value.Required,
		ContentType: contentType,
		Schema:      schemaDigest(mediaType.Schema),
	}
	if mediaType.Schema != nil && mediaType.Schema.Value != nil && mediaType.Schema.Value.Type == "object" {
		required := make(map[string]struct{}, len(mediaType.Schema.Value.Required))
		for _, name := range mediaType.Schema.Value.Required {
			required[name] = struct{}{}
		}
		names := make([]string, 0, len(mediaType.Schema.Value.Properties))
		for name := range mediaType.Schema.Value.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			propertyRef := mediaType.Schema.Value.Properties[name]
			if propertyRef == nil || propertyRef.Value == nil {
				continue
			}
			_, isRequired := required[name]
			out.Fields = append(out.Fields, specindex.Parameter{
				Name:        name,
				In:          "body",
				Required:    isRequired,
				Description: flatten(strings.TrimSpace(propertyRef.Value.Description)),
				Schema:      schemaDigest(propertyRef),
				Default:     defaultFromSchema(propertyRef),
			})
		}
	}
	return out, nil
}

func buildResponses(responses *openapi3.Responses) ([]specindex.Response, error) {
	if responses == nil {
		return nil, nil
	}
	keys := make([]string, 0, responses.Len())
	for key := range responses.Map() {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return responseSortKey(keys[i]) < responseSortKey(keys[j])
	})
	out := make([]specindex.Response, 0, len(keys))
	for _, key := range keys {
		ref := responses.Value(key)
		if ref == nil || ref.Value == nil {
			continue
		}
		response := specindex.Response{
			Status:      key,
			Description: flatten(strings.TrimSpace(stringValue(ref.Value.Description))),
		}
		if len(ref.Value.Content) > 0 {
			contentTypes := make([]string, 0, len(ref.Value.Content))
			for contentType := range ref.Value.Content {
				contentTypes = append(contentTypes, contentType)
			}
			sort.Strings(contentTypes)
			response.ContentTypes = contentTypes
			for _, contentType := range contentTypes {
				mediaType := ref.Value.Content[contentType]
				if mediaType == nil {
					continue
				}
				if response.Schema == nil {
					response.Schema = schemaDigest(mediaType.Schema)
				}
				if isBinaryContent(contentType, mediaType.Schema) {
					response.Binary = true
				}
			}
		}
		out = append(out, response)
	}
	return out, nil
}

func schemaDigest(ref *openapi3.SchemaRef) *specindex.SchemaDigest {
	return schemaDigestWithSeen(ref, map[*openapi3.Schema]struct{}{})
}

func schemaDigestWithSeen(ref *openapi3.SchemaRef, seen map[*openapi3.Schema]struct{}) *specindex.SchemaDigest {
	if ref == nil || ref.Value == nil {
		return nil
	}
	schema := ref.Value
	digest := &specindex.SchemaDigest{
		Type:        schema.Type,
		Format:      schema.Format,
		Title:       strings.TrimSpace(schema.Title),
		Description: flatten(strings.TrimSpace(schema.Description)),
	}
	if _, ok := seen[schema]; ok {
		return digest
	}
	seen[schema] = struct{}{}
	defer delete(seen, schema)
	if len(schema.Enum) > 0 {
		digest.Enum = make([]string, 0, len(schema.Enum))
		for _, item := range schema.Enum {
			digest.Enum = append(digest.Enum, fmt.Sprint(item))
		}
	}
	if schema.Items != nil {
		digest.Items = schemaDigestWithSeen(schema.Items, seen)
	}
	if len(schema.Properties) > 0 {
		required := make(map[string]struct{}, len(schema.Required))
		for _, name := range schema.Required {
			required[name] = struct{}{}
		}
		names := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			child := schemaDigestWithSeen(schema.Properties[name], seen)
			if child == nil {
				continue
			}
			_, isRequired := required[name]
			digest.Properties = append(digest.Properties, specindex.SchemaProperty{
				Name:     name,
				Required: isRequired,
				Schema:   child,
			})
		}
	}
	for _, child := range schema.OneOf {
		digest.OneOf = append(digest.OneOf, schemaDigestWithSeen(child, seen))
	}
	for _, child := range schema.AnyOf {
		digest.AnyOf = append(digest.AnyOf, schemaDigestWithSeen(child, seen))
	}
	for _, child := range schema.AllOf {
		digest.AllOf = append(digest.AllOf, schemaDigestWithSeen(child, seen))
	}
	mergeExampleShape(digest, schema.Example)
	return digest
}

func mergeExampleShape(digest *specindex.SchemaDigest, example any) {
	if digest == nil || example == nil {
		return
	}
	inferred := schemaDigestFromExample(example)
	if inferred == nil {
		return
	}
	if digest.Type == "" && inferred.Type != "" {
		digest.Type = inferred.Type
	}
	if digest.Type == "object" && len(digest.Properties) == 0 && len(inferred.Properties) > 0 {
		digest.Properties = inferred.Properties
	}
	if digest.Type == "array" && digest.Items == nil && inferred.Items != nil {
		digest.Items = inferred.Items
	}
}

func schemaDigestFromExample(example any) *specindex.SchemaDigest {
	if example == nil {
		return nil
	}
	switch value := example.(type) {
	case string:
		return &specindex.SchemaDigest{Type: "string"}
	case bool:
		return &specindex.SchemaDigest{Type: "boolean"}
	case int, int8, int16, int32, int64:
		return &specindex.SchemaDigest{Type: "integer"}
	case uint, uint8, uint16, uint32, uint64:
		return &specindex.SchemaDigest{Type: "integer"}
	case float32:
		if math.Trunc(float64(value)) == float64(value) {
			return &specindex.SchemaDigest{Type: "integer"}
		}
		return &specindex.SchemaDigest{Type: "number"}
	case float64:
		if math.Trunc(value) == value {
			return &specindex.SchemaDigest{Type: "integer"}
		}
		return &specindex.SchemaDigest{Type: "number"}
	}

	rv := reflect.ValueOf(example)
	switch rv.Kind() {
	case reflect.Map:
		keys := make([]string, 0, rv.Len())
		values := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			name := fmt.Sprint(iter.Key().Interface())
			keys = append(keys, name)
			values[name] = iter.Value().Interface()
		}
		sort.Strings(keys)
		properties := make([]specindex.SchemaProperty, 0, len(keys))
		for _, name := range keys {
			properties = append(properties, specindex.SchemaProperty{
				Name:   name,
				Schema: schemaDigestFromExample(values[name]),
			})
		}
		return &specindex.SchemaDigest{
			Type:       "object",
			Properties: properties,
		}
	case reflect.Slice, reflect.Array:
		digest := &specindex.SchemaDigest{Type: "array"}
		for index := 0; index < rv.Len(); index++ {
			child := schemaDigestFromExample(rv.Index(index).Interface())
			if child != nil {
				digest.Items = child
				break
			}
		}
		return digest
	default:
		return nil
	}
}

func defaultFromSchema(ref *openapi3.SchemaRef) any {
	if ref == nil || ref.Value == nil {
		return nil
	}
	return ref.Value.Default
}

func pickContent(content openapi3.Content) (string, *openapi3.MediaType) {
	for _, preferred := range contentTypePreference {
		if mediaType, ok := content[preferred]; ok {
			return preferred, mediaType
		}
	}
	keys := make([]string, 0, len(content))
	for key := range content {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "", nil
	}
	return keys[0], content[keys[0]]
}

func isBinaryContent(contentType string, schema *openapi3.SchemaRef) bool {
	if strings.HasPrefix(contentType, "image/") || contentType == "application/octet-stream" {
		return true
	}
	return schema != nil && schema.Value != nil && schema.Value.Type == "string" && schema.Value.Format == "binary"
}

func buildAliases(op *specindex.Operation) []string {
	aliases := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			aliases[value] = struct{}{}
		}
	}
	add(op.OperationID)
	add(strings.ReplaceAll(op.OperationID, "_", "-"))
	add(strings.ReplaceAll(op.OperationID, "-", "_"))
	cleanPath := strings.TrimPrefix(strings.Trim(op.Path, "/"), "api/v1/")
	add(cleanPath)
	add(strings.ToLower(op.Method) + ":" + cleanPath)
	pathParts := make([]string, 0)
	for _, part := range strings.Split(cleanPath, "/") {
		if part == "" || strings.HasPrefix(part, "{") {
			continue
		}
		pathParts = append(pathParts, part)
	}
	if len(pathParts) > 0 {
		add(strings.Join(pathParts, "."))
		add(strings.Join(pathParts, "/"))
		if len(op.Tags) > 0 {
			add(strings.ToLower(op.Tags[0]) + "." + pathParts[len(pathParts)-1])
			start := len(pathParts) - 2
			if start < 0 {
				start = 0
			}
			add(strings.ToLower(op.Tags[0]) + "." + strings.Join(pathParts[start:], "."))
		}
	}
	out := make([]string, 0, len(aliases))
	for alias := range aliases {
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func responseSortKey(status string) string {
	if len(status) == 3 && status[0] >= '0' && status[0] <= '9' {
		return "0:" + status
	}
	return "1:" + status
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func flatten(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizeOperationPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return apiPrefix
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasPrefix(path, apiPrefix+"/") || path == apiPrefix {
		return path
	}
	return apiPrefix + path
}
