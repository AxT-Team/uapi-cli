package specbuild

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestSchemaDigestPreservesPropertiesAndVariants(t *testing.T) {
	digest := schemaDigest(&openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:        "object",
			Description: "Root response.",
			Required:    []string{"text"},
			Properties: openapi3.Schemas{
				"failure": {
					Value: &openapi3.Schema{
						OneOf: openapi3.SchemaRefs{
							{
								Value: &openapi3.Schema{
									Title: "Missing image source",
									Type:  "object",
									Properties: openapi3.Schemas{
										"code": {Value: &openapi3.Schema{Type: "string"}},
									},
								},
							},
						},
					},
				},
				"image": {
					Value: &openapi3.Schema{
						Type:    "object",
						Example: map[string]any{"width": 1280, "height": 720},
					},
				},
				"text": {
					Value: &openapi3.Schema{
						Type:        "string",
						Description: "Joined OCR text.",
					},
				},
			},
		},
	})

	if digest == nil {
		t.Fatal("expected digest")
	}
	if digest.Type != "object" {
		t.Fatalf("digest type = %q, want object", digest.Type)
	}
	if digest.Description != "Root response." {
		t.Fatalf("digest description = %q", digest.Description)
	}
	if len(digest.Properties) != 3 {
		t.Fatalf("property count = %d, want 3", len(digest.Properties))
	}
	if digest.Properties[0].Name != "failure" {
		t.Fatalf("first property = %q, want failure", digest.Properties[0].Name)
	}
	if digest.Properties[1].Name != "image" {
		t.Fatalf("second property = %q, want image", digest.Properties[1].Name)
	}
	if digest.Properties[2].Name != "text" {
		t.Fatalf("third property = %q, want text", digest.Properties[2].Name)
	}
	image := digest.Properties[1].Schema
	if image == nil || len(image.Properties) != 2 {
		t.Fatalf("image properties = %#v", image)
	}
	if image.Properties[0].Name != "height" || image.Properties[1].Name != "width" {
		t.Fatalf("image property names = %#v", image.Properties)
	}
	if !digest.Properties[2].Required {
		t.Fatal("text should be marked required")
	}
	if digest.Properties[2].Schema == nil || digest.Properties[2].Schema.Description != "Joined OCR text." {
		t.Fatalf("text description = %#v", digest.Properties[2].Schema)
	}
	failure := digest.Properties[0].Schema
	if failure == nil || len(failure.OneOf) != 1 {
		t.Fatalf("failure variants = %#v", failure)
	}
	if failure.OneOf[0].Title != "Missing image source" {
		t.Fatalf("variant title = %q", failure.OneOf[0].Title)
	}
	if len(failure.OneOf[0].Properties) != 1 || failure.OneOf[0].Properties[0].Name != "code" {
		t.Fatalf("variant properties = %#v", failure.OneOf[0].Properties)
	}
}
