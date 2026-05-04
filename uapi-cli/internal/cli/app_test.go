package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/AxT-Team/uapi-cli/internal/specindex"
)

func TestSplitCallArgsInterspersedFlags(t *testing.T) {
	operation, flagArgs, err := splitCallArgs([]string{
		"--base-url", "https://uapis.cn",
		"get-social-bilibili-userinfo",
		"--set", "uid=1945126",
		"--disable-cache",
	})
	if err != nil {
		t.Fatalf("splitCallArgs returned error: %v", err)
	}
	if operation != "get-social-bilibili-userinfo" {
		t.Fatalf("unexpected operation: %q", operation)
	}
	expected := []string{"--base-url", "https://uapis.cn", "--set", "uid=1945126", "--disable-cache"}
	if len(flagArgs) != len(expected) {
		t.Fatalf("unexpected flag arg count: got %d want %d", len(flagArgs), len(expected))
	}
	for index := range expected {
		if flagArgs[index] != expected[index] {
			t.Fatalf("flag arg mismatch at %d: got %q want %q", index, flagArgs[index], expected[index])
		}
	}
}

func TestSplitCallArgsRecognizesAPIKeyAlias(t *testing.T) {
	operation, flagArgs, err := splitCallArgs([]string{
		"--api-key", "secret-key",
		"post-image-ocr",
		"--body", "url=https://uapis.cn/ocr-samples/bilingual-poetry-sample.png",
	})
	if err != nil {
		t.Fatalf("splitCallArgs returned error: %v", err)
	}
	if operation != "post-image-ocr" {
		t.Fatalf("unexpected operation: %q", operation)
	}
	expected := []string{
		"--api-key", "secret-key",
		"--body", "url=https://uapis.cn/ocr-samples/bilingual-poetry-sample.png",
	}
	if len(flagArgs) != len(expected) {
		t.Fatalf("unexpected flag arg count: got %d want %d", len(flagArgs), len(expected))
	}
	for index := range expected {
		if flagArgs[index] != expected[index] {
			t.Fatalf("flag arg mismatch at %d: got %q want %q", index, flagArgs[index], expected[index])
		}
	}
}

func TestParseCallFlagsUsesUAPIKeyEnvFallback(t *testing.T) {
	t.Setenv("UAPI_TOKEN", "")
	t.Setenv("UAPI_KEY", "env-key")
	app := &App{stderr: &strings.Builder{}}
	cfg, _, operationName, err := app.parseCallFlags([]string{"get-social-bilibili-userinfo", "--set", "uid=1945126"})
	if err != nil {
		t.Fatalf("parseCallFlags returned error: %v", err)
	}
	if operationName != "get-social-bilibili-userinfo" {
		t.Fatalf("unexpected operation: %q", operationName)
	}
	if cfg.Token != "env-key" {
		t.Fatalf("expected env token fallback, got %q", cfg.Token)
	}
}

func TestParseTypedValueKeepsLeadingZeroStrings(t *testing.T) {
	value := parseTypedValue("012345")
	text, ok := value.(string)
	if !ok {
		t.Fatalf("expected string, got %T", value)
	}
	if text != "012345" {
		t.Fatalf("unexpected string value: %q", text)
	}
}

func TestParseTypedValueParsesIntegersWithoutLeadingZeros(t *testing.T) {
	value := parseTypedValue("1945126")
	number, ok := value.(json.Number)
	if !ok {
		t.Fatalf("expected json.Number, got %T", value)
	}
	if number.String() != "1945126" {
		t.Fatalf("unexpected numeric value: %s", number.String())
	}
}

func TestShouldTreatResponseAsBinaryPrefersActualJSONContentType(t *testing.T) {
	op := &specindex.Operation{
		Responses: []specindex.Response{
			{
				Status:       "200",
				Binary:       true,
				ContentTypes: []string{"application/json", "image/jpeg"},
			},
		},
	}
	if shouldTreatResponseAsBinary(op, "application/json") {
		t.Fatal("application/json should not be treated as binary")
	}
	if !shouldTreatResponseAsBinary(op, "image/jpeg") {
		t.Fatal("image/jpeg should be treated as binary")
	}
}

func TestParseTypedValueKeepsFloatLexeme(t *testing.T) {
	value := parseTypedValue("1.0")
	number, ok := value.(json.Number)
	if !ok {
		t.Fatalf("expected json.Number, got %T", value)
	}
	if number.String() != "1.0" {
		t.Fatalf("unexpected numeric lexeme: %s", number.String())
	}
}

func TestResolveFormatUsesLLMForNonTTY(t *testing.T) {
	var writer strings.Builder
	if got := resolveFormat("auto", commandModeDiscover, &writer); got != "llm" {
		t.Fatalf("discover auto format = %q, want llm", got)
	}
	if got := resolveFormat("auto", commandModeCall, &writer); got != "llm" {
		t.Fatalf("call auto format = %q, want llm", got)
	}
}

func TestCallPayloadOmitsExpandedOperationAndRawHeaders(t *testing.T) {
	request := &builtRequest{
		Operation:   &specindex.Operation{OperationID: "get-network-myip"},
		OperationID: "get-network-myip",
		Method:      "GET",
		Path:        "/api/v1/network/myip",
		URL:         "https://uapis.cn/api/v1/network/myip",
		Query:       map[string]string{"_t": "123"},
	}
	envelope := map[string]any{
		"ok": true,
		"operation": map[string]any{
			"operation_id": "get-network-myip",
			"method":       "GET",
			"path":         "/api/v1/network/myip",
		},
		"request": request,
		"response": map[string]any{
			"status":       200,
			"content_type": "application/json",
			"binary":       false,
			"data":         map[string]any{"ip": "1.2.3.4"},
			"meta": responseMeta{
				RequestID:  "req_123",
				RawHeaders: map[string]string{"x-request-id": "req_123"},
			},
		},
	}

	payload := callPayload(envelope)
	gotRequest, ok := payload["request"].(map[string]any)
	if !ok {
		t.Fatalf("request payload type = %T", payload["request"])
	}
	if _, exists := gotRequest["operation"]; exists {
		t.Fatal("request payload should not include expanded operation")
	}
	gotResponse, ok := payload["response"].(map[string]any)
	if !ok {
		t.Fatalf("response payload type = %T", payload["response"])
	}
	gotMeta, ok := gotResponse["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta payload type = %T", gotResponse["meta"])
	}
	if _, exists := gotMeta["raw_headers"]; exists {
		t.Fatal("llm response meta should omit raw_headers")
	}
}

func TestRenderOperationsPrettyIncludesOfficialBranding(t *testing.T) {
	output := renderOperationsPretty([]*specindex.Operation{
		{
			OperationID: "post-image-ocr",
			Method:      "POST",
			Path:        "/api/v1/image/ocr",
			Summary:     "General OCR text recognition",
			Tags:        []string{"Image"},
			Aliases:     []string{"image.ocr"},
		},
	})
	if !strings.Contains(output, "https://uapis.cn") {
		t.Fatalf("expected official branding header, got: %s", output)
	}
}

func TestRenderSchemaPrettyExpandsResponses(t *testing.T) {
	output := renderSchemaPretty(&specindex.Operation{
		OperationID: "post-image-ocr",
		Method:      "POST",
		Path:        "/api/v1/image/ocr",
		Tags:        []string{"Image"},
		Responses: []specindex.Response{
			{
				Status:       "200",
				ContentTypes: []string{"application/json"},
				Schema: &specindex.SchemaDigest{
					Type: "object",
					Properties: []specindex.SchemaProperty{
						{
							Name: "text",
							Schema: &specindex.SchemaDigest{
								Type:        "string",
								Description: "Joined OCR text.",
							},
						},
						{
							Name: "words_result",
							Schema: &specindex.SchemaDigest{
								Type:        "array",
								Description: "Per-block OCR results.",
								Items: &specindex.SchemaDigest{
									Type: "object",
									Properties: []specindex.SchemaProperty{
										{
											Name: "location",
											Schema: &specindex.SchemaDigest{
												Type:        "object",
												Description: "Bounding box.",
												Properties: []specindex.SchemaProperty{
													{Name: "left", Required: true, Schema: &specindex.SchemaDigest{Type: "number"}},
													{Name: "top", Schema: &specindex.SchemaDigest{Type: "number"}},
												},
											},
										},
										{
											Name: "words",
											Schema: &specindex.SchemaDigest{
												Type:        "string",
												Description: "Recognized text.",
											},
										},
									},
								},
							},
						},
					},
				},
				Description: "Successful OCR response.",
			},
		},
	})
	for _, want := range []string{
		"https://uapis.cn",
		"responses",
		"  - 200",
		"    content-types: application/json",
		"    body: object",
		"    fields",
		"      - text (string) :: Joined OCR text.",
		"      - words_result (array<object>) :: Per-block OCR results.",
		"        - item (object)",
		"          - location (object) :: Bounding box.",
		"            - left (number, required)",
		"          - words (string) :: Recognized text.",
		"    summary: Successful OCR response.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output:\n%s", want, output)
		}
	}
}

func TestRenderSchemaPrettyExpandsOneOfResponseVariants(t *testing.T) {
	output := renderSchemaPretty(&specindex.Operation{
		OperationID: "post-image-ocr",
		Method:      "POST",
		Path:        "/api/v1/image/ocr",
		Tags:        []string{"Image"},
		Responses: []specindex.Response{
			{
				Status:       "400",
				ContentTypes: []string{"application/json"},
				Schema: &specindex.SchemaDigest{
					OneOf: []*specindex.SchemaDigest{
						{
							Title: "Missing image source",
							Type:  "object",
							Properties: []specindex.SchemaProperty{
								{Name: "code", Schema: &specindex.SchemaDigest{Type: "string"}},
								{Name: "message", Schema: &specindex.SchemaDigest{Type: "string"}},
							},
						},
						{
							Title: "Conflicting image source",
							Type:  "object",
							Properties: []specindex.SchemaProperty{
								{Name: "code", Schema: &specindex.SchemaDigest{Type: "string"}},
								{Name: "message", Schema: &specindex.SchemaDigest{Type: "string"}},
							},
						},
					},
				},
				Description: "Invalid request variants.",
			},
		},
	})
	for _, want := range []string{
		"    variants",
		"      - variant 1: Missing image source (object)",
		"        - code (string)",
		"      - variant 2: Conflicting image source (object)",
		"    summary: Invalid request variants.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output:\n%s", want, output)
		}
	}
}

func TestSplitFirstPositionalKeepsFlagsAfterOperation(t *testing.T) {
	positional, flags := splitFirstPositional([]string{"post-image-ocr", "--format", "pretty"})
	if positional != "post-image-ocr" {
		t.Fatalf("unexpected positional: %q", positional)
	}
	expected := []string{"--format", "pretty"}
	if len(flags) != len(expected) {
		t.Fatalf("unexpected flag count: got %d want %d", len(flags), len(expected))
	}
	for i := range expected {
		if flags[i] != expected[i] {
			t.Fatalf("flag mismatch at %d: got %q want %q", i, flags[i], expected[i])
		}
	}
}

func TestMapAPIErrorPromotesVisitorQuotaExhausted(t *testing.T) {
	zero := int64(0)
	err := mapAPIError(http.StatusTooManyRequests, []byte(`{"message":"visitor monthly quota exhausted"}`), responseMeta{
		VisitorQuotaRemainingCredits: &zero,
	})
	apiErr, ok := err.(*apiError)
	if !ok {
		t.Fatalf("expected *apiError, got %T", err)
	}
	if apiErr.Code != "VISITOR_MONTHLY_QUOTA_EXHAUSTED" {
		t.Fatalf("unexpected code: %q", apiErr.Code)
	}
}

func TestApiErrorForOutputIncludesVisitorQuotaGuidance(t *testing.T) {
	zero := int64(0)
	apiErr := &apiError{
		Code:    "VISITOR_MONTHLY_QUOTA_EXHAUSTED",
		Status:  http.StatusTooManyRequests,
		Message: "visitor monthly quota exhausted",
		Meta: responseMeta{
			VisitorQuotaRemainingCredits: &zero,
		},
	}
	payload := apiErrorForOutput(apiErr, true, "post-image-ocr")
	if got := payload["kind"]; got != "visitor_quota_exhausted" {
		t.Fatalf("unexpected kind: %v", got)
	}
	hint, ok := payload["hint"].(string)
	if !ok || !strings.Contains(hint, "--api-key") || !strings.Contains(hint, "UAPI_TOKEN") {
		t.Fatalf("unexpected hint: %#v", payload["hint"])
	}
	actions, ok := payload["actions"].([]apiErrorAction)
	if !ok {
		t.Fatalf("unexpected actions type: %T", payload["actions"])
	}
	rendered := compactJSON(actions)
	for _, want := range []string{officialConsoleURL, officialPricingURL, "--api-key", "UAPI_TOKEN"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected %q in actions: %s", want, rendered)
		}
	}
}

func TestApiErrorForOutputIncludesBalanceAndQuotaMeta(t *testing.T) {
	zero := int64(0)
	quota := int64(12)
	payload := apiErrorForOutput(&apiError{
		Code:    "INSUFFICIENT_CREDITS",
		Status:  http.StatusPaymentRequired,
		Message: "insufficient credits",
		Meta: responseMeta{
			BalanceRemainingCents: &zero,
			QuotaRemainingCredits: &zero,
			QuotaLimitCredits:     &quota,
		},
	}, true, "post-image-ocr")
	meta, ok := payload["meta"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected meta type: %T", payload["meta"])
	}
	if got := meta["balance_remaining_cents"]; got != int64(0) {
		t.Fatalf("unexpected balance_remaining_cents: %#v", got)
	}
	if got := meta["quota_remaining_credits"]; got != int64(0) {
		t.Fatalf("unexpected quota_remaining_credits: %#v", got)
	}
	if got := meta["quota_limit_credits"]; got != int64(12) {
		t.Fatalf("unexpected quota_limit_credits: %#v", got)
	}
}

func TestRenderAPIErrorPrettyShowsInsufficientCreditsGuidance(t *testing.T) {
	zero := int64(0)
	requested := int64(8)
	apiErr := &apiError{
		Code:    "INSUFFICIENT_CREDITS",
		Status:  http.StatusPaymentRequired,
		Message: "insufficient credits",
		Meta: responseMeta{
			CreditsRequested:      &requested,
			BalanceRemainingCents: &zero,
			QuotaRemainingCredits: &zero,
		},
	}
	output := renderAPIErrorPretty("post-image-ocr", apiErr)
	for _, want := range []string{
		"https://uapis.cn",
		officialConsoleURL,
		officialPricingURL,
		"--api-key",
		"credits requested: 8",
		"balance remaining: ¥0.00 (0 cents)",
		"quota remaining: 0 credits",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output:\n%s", want, output)
		}
	}
}

func TestRenderAPIErrorPrettyShowsUnauthorizedGuidance(t *testing.T) {
	output := renderAPIErrorPretty("post-image-ocr", &apiError{
		Code:    "UNAUTHORIZED",
		Status:  http.StatusUnauthorized,
		Message: "missing token",
	})
	for _, want := range []string{
		officialConsoleURL,
		"--api-key",
		"UAPI_TOKEN",
		"Missing or invalid UAPI Key",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output:\n%s", want, output)
		}
	}
}
