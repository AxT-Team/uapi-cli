package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AxT-Team/uapi-cli/internal/specindex"
)

const (
	defaultBaseURL     = "https://uapis.cn"
	apiPrefix          = "/api/v1"
	officialHost       = "uapis.cn"
	officialDocsURL    = "https://uapis.cn/docs/introduction"
	officialConsoleURL = "https://uapis.cn/console"
	officialPricingURL = "https://uapis.cn/pricing"
	officialASCII      = "   __  __            _ ____           \n" +
		"  / / / /___ _____  (_) __ \\\\_________ \n" +
		" / / / / __ `/ __ \\/ / /_/ / ___/ __ \\\n" +
		"/ /_/ / /_/ / /_/ / / ____/ /  / /_/ /\n" +
		"\\____/\\__,_/ .___/_/_/   /_/   \\____/ \n" +
		"          /_/                         "
)

type App struct {
	in     io.Reader
	stdout io.Writer
	stderr io.Writer
	index  *specindex.Index
}

type requestConfig struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

type CallOptions struct {
	Set          multiValue
	Path         multiValue
	Query        multiValue
	Body         multiValue
	Header       multiValue
	File         multiValue
	JSONText     string
	JSONFile     string
	STDINJSON    bool
	DisableCache bool
	CacheBuster  string
	Out          string
	DryRun       bool
	Format       string
}

type multiValue []string

func (value *multiValue) String() string {
	return strings.Join(*value, ",")
}

func (value *multiValue) Set(input string) error {
	*value = append(*value, input)
	return nil
}

type apiClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type apiError struct {
	Code    string       `json:"code"`
	Status  int          `json:"status"`
	Message string       `json:"message"`
	Details any          `json:"details,omitempty"`
	Payload any          `json:"payload,omitempty"`
	Meta    responseMeta `json:"meta"`
}

func (err *apiError) Error() string {
	return fmt.Sprintf("[%d] %s: %s", err.Status, err.Code, err.Message)
}

type apiErrorAction struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Example     string `json:"example,omitempty"`
}

type apiErrorGuidance struct {
	Kind    string
	Title   string
	Summary string
	Actions []apiErrorAction
}

type responseMeta struct {
	RequestID                    string                          `json:"request_id,omitempty"`
	RetryAfterSeconds            *int64                          `json:"retry_after_seconds,omitempty"`
	DebitStatus                  string                          `json:"debit_status,omitempty"`
	CreditsRequested             *int64                          `json:"credits_requested,omitempty"`
	CreditsCharged               *int64                          `json:"credits_charged,omitempty"`
	CreditsPricing               string                          `json:"credits_pricing,omitempty"`
	ActiveQuotaBuckets           *int64                          `json:"active_quota_buckets,omitempty"`
	StopOnEmpty                  *bool                           `json:"stop_on_empty,omitempty"`
	RateLimitPolicyRaw           string                          `json:"rate_limit_policy_raw,omitempty"`
	RateLimitRaw                 string                          `json:"rate_limit_raw,omitempty"`
	RateLimitPolicies            map[string]rateLimitPolicyEntry `json:"rate_limit_policies,omitempty"`
	RateLimits                   map[string]rateLimitStateEntry  `json:"rate_limits,omitempty"`
	BalanceLimitCents            *int64                          `json:"balance_limit_cents,omitempty"`
	BalanceRemainingCents        *int64                          `json:"balance_remaining_cents,omitempty"`
	QuotaLimitCredits            *int64                          `json:"quota_limit_credits,omitempty"`
	QuotaRemainingCredits        *int64                          `json:"quota_remaining_credits,omitempty"`
	VisitorQuotaLimitCredits     *int64                          `json:"visitor_quota_limit_credits,omitempty"`
	VisitorQuotaRemainingCredits *int64                          `json:"visitor_quota_remaining_credits,omitempty"`
	RawHeaders                   map[string]string               `json:"raw_headers,omitempty"`
}

type rateLimitPolicyEntry struct {
	Name          string `json:"name"`
	Quota         *int64 `json:"quota,omitempty"`
	Unit          string `json:"unit,omitempty"`
	WindowSeconds *int64 `json:"window_seconds,omitempty"`
}

type rateLimitStateEntry struct {
	Name              string `json:"name"`
	Remaining         *int64 `json:"remaining,omitempty"`
	Unit              string `json:"unit,omitempty"`
	ResetAfterSeconds *int64 `json:"reset_after_seconds,omitempty"`
}

type builtRequest struct {
	Operation   *specindex.Operation `json:"-"`
	OperationID string               `json:"operation_id"`
	Method      string               `json:"method"`
	Path        string               `json:"path"`
	URL         string               `json:"url"`
	Headers     map[string]string    `json:"headers,omitempty"`
	Query       map[string]string    `json:"query,omitempty"`
	BodyKind    string               `json:"body_kind,omitempty"`
	JSONBody    any                  `json:"json_body,omitempty"`
	TextBody    string               `json:"text_body,omitempty"`
	FormBody    map[string]string    `json:"form_body,omitempty"`
	FileFields  map[string]string    `json:"file_fields,omitempty"`
}

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	index, err := specindex.LoadEmbedded()
	if err != nil {
		return err
	}
	app := &App{in: stdin, stdout: stdout, stderr: stderr, index: index}
	return app.run(args)
}

func (app *App) run(args []string) error {
	if len(args) == 0 {
		app.printRootHelp()
		return nil
	}
	switch args[0] {
	case "-h", "--help", "help":
		app.printRootHelp()
		return nil
	case "discover":
		return app.runDiscover(args[1:])
	case "tags":
		return app.runTags(args[1:])
	case "schema":
		return app.runSchema(args[1:])
	case "call":
		return app.runCall(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (app *App) runDiscover(args []string) error {
	query, flagArgs := splitFirstPositional(args)
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	fs.SetOutput(app.stderr)
	format := fs.String("format", "auto", "auto|pretty|json|llm")
	tag := fs.String("tag", "", "Filter by tag")
	method := fs.String("method", "", "Filter by HTTP method")
	limit := fs.Int("limit", 20, "Result limit")
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if query == "" && fs.NArg() > 0 {
		query = fs.Arg(0)
	}
	ops := app.index.Search(query, *tag, *method, *limit)
	outputFormat := resolveFormat(*format, commandModeDiscover, app.stdout)
	payload := map[string]any{"items": ops}
	if outputFormat == "llm" {
		payload = discoverPayload(query, *tag, *method, ops)
	}
	return app.writePayload(payload, outputFormat, func() string {
		return renderOperationsPretty(ops)
	})
}

func (app *App) runTags(args []string) error {
	_, flagArgs := splitFirstPositional(args)
	fs := flag.NewFlagSet("tags", flag.ContinueOnError)
	fs.SetOutput(app.stderr)
	format := fs.String("format", "auto", "auto|pretty|json|llm")
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	outputFormat := resolveFormat(*format, commandModeTags, app.stdout)
	payload := map[string]any{"items": app.index.Tags}
	if outputFormat == "llm" {
		payload = tagsPayload(app.index.Tags)
	}
	return app.writePayload(payload, outputFormat, func() string {
		return renderTagsPretty(app.index.Tags)
	})
}

func (app *App) runSchema(args []string) error {
	operationName, flagArgs := splitFirstPositional(args)
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	fs.SetOutput(app.stderr)
	format := fs.String("format", "auto", "auto|pretty|json|llm")
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if operationName == "" && fs.NArg() > 0 {
		operationName = fs.Arg(0)
	}
	if operationName == "" {
		return errors.New("schema requires an operation")
	}
	op, err := app.index.ResolveOperation(operationName)
	if err != nil {
		return err
	}
	outputFormat := resolveFormat(*format, commandModeSchema, app.stdout)
	payload := any(op)
	if outputFormat == "llm" {
		payload = schemaPayload(op)
	}
	return app.writePayload(payload, outputFormat, func() string {
		return renderSchemaPretty(op)
	})
}

func (app *App) runCall(args []string) error {
	cfg, opts, operationName, err := app.parseCallFlags(args)
	if err != nil {
		return err
	}
	op, err := app.index.ResolveOperation(operationName)
	if err != nil {
		return err
	}

	pathValues, queryValues, bodyValues, headerValues, fileValues, jsonPayload, err := collectCallInputs(app.in, op, opts)
	if err != nil {
		return err
	}

	client := newAPIClient(cfg)
	request, err := client.buildRequest(op, pathValues, queryValues, bodyValues, headerValues, fileValues, jsonPayload, opts.DisableCache, opts.CacheBuster)
	if err != nil {
		return err
	}

	outputFormat := resolveFormat(opts.Format, commandModeCall, app.stdout)
	if opts.DryRun {
		payload := map[string]any{
			"ok":      true,
			"dry_run": true,
			"request": request,
		}
		if outputFormat == "llm" {
			payload = dryRunPayload(request)
		}
		return app.writePayload(payload, outputFormat, func() string {
			return prettyJSON(payload)
		})
	}

	envelope, rawBytes, err := client.execute(context.Background(), request, opts.Out)
	if err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			payload := map[string]any{
				"ok":    false,
				"error": apiErrorForOutput(apiErr, outputFormat == "llm", operationName),
			}
			_ = app.writePayloadTo(app.stderr, payload, errorOutputFormat(outputFormat), func() string {
				return renderAPIErrorPretty(operationName, apiErr)
			})
			return errSilent{}
		}
		payload := map[string]any{
			"ok": false,
			"error": map[string]any{
				"type":    "CliError",
				"message": err.Error(),
			},
		}
		_ = app.writePayloadTo(app.stderr, payload, errorOutputFormat(outputFormat), func() string {
			return renderCLIErrorPretty(err)
		})
		return errSilent{}
	}

	if outputFormat == "raw" {
		response := envelope["response"].(map[string]any)
		if binary, _ := response["binary"].(bool); binary {
			_, _ = fmt.Fprint(app.stdout, response["saved_to"])
			if isTTY(app.stdout) {
				_, _ = fmt.Fprint(app.stdout, "\n")
			}
			return nil
		}
		if len(rawBytes) > 0 {
			_, _ = app.stdout.Write(rawBytes)
			return nil
		}
		if text, ok := response["text"].(string); ok {
			_, _ = io.WriteString(app.stdout, text)
			if isTTY(app.stdout) {
				_, _ = io.WriteString(app.stdout, "\n")
			}
		}
		return nil
	}

	payload := any(envelope)
	if outputFormat == "llm" {
		payload = callPayload(envelope)
	}
	return app.writePayload(payload, outputFormat, func() string {
		return renderCallPretty(envelope)
	})
}

func (app *App) parseCallFlags(args []string) (requestConfig, CallOptions, string, error) {
	operationName, flagArgs, err := splitCallArgs(args)
	if err != nil {
		return requestConfig{}, CallOptions{}, "", err
	}
	fs := flag.NewFlagSet("call", flag.ContinueOnError)
	fs.SetOutput(app.stderr)
	fs.Usage = func() {
		app.printCallHelp()
	}
	baseURL := fs.String("base-url", getenvDefault("UAPI_BASE_URL", defaultBaseURL), "Base URL")
	tokenValue := defaultTokenValue()
	fs.StringVar(&tokenValue, "token", tokenValue, "UAPI Key / token")
	fs.StringVar(&tokenValue, "api-key", tokenValue, "UAPI Key")
	fs.StringVar(&tokenValue, "key", tokenValue, "UAPI Key")
	timeoutSeconds := fs.Float64("timeout", getenvFloat("UAPI_TIMEOUT", 30), "Request timeout in seconds")
	var opts CallOptions
	fs.Var(&opts.Set, "set", "KEY=VALUE auto-routed input")
	fs.Var(&opts.Path, "path", "KEY=VALUE path input")
	fs.Var(&opts.Query, "query", "KEY=VALUE query input")
	fs.Var(&opts.Body, "body", "KEY=VALUE body input")
	fs.Var(&opts.Header, "header", "KEY=VALUE header input")
	fs.Var(&opts.File, "file", "FIELD=PATH multipart file input")
	fs.StringVar(&opts.JSONText, "json", "", "Inline JSON input")
	fs.StringVar(&opts.JSONFile, "json-file", "", "JSON file input")
	fs.BoolVar(&opts.STDINJSON, "stdin-json", false, "Read JSON input from stdin")
	fs.BoolVar(&opts.DisableCache, "disable-cache", false, "Add _t automatically when not already provided")
	fs.StringVar(&opts.CacheBuster, "cache-buster", "", "Explicit _t value")
	fs.StringVar(&opts.Out, "out", "", "Output file for binary responses")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "Print normalized request without executing it")
	fs.StringVar(&opts.Format, "format", "auto", "auto|json|llm|pretty|raw")
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return requestConfig{}, CallOptions{}, "", errSilent{}
		}
		return requestConfig{}, CallOptions{}, "", err
	}
	if operationName == "" {
		return requestConfig{}, CallOptions{}, "", errors.New("call requires an operation")
	}
	if fs.NArg() > 0 {
		return requestConfig{}, CallOptions{}, "", fmt.Errorf("unexpected positional arguments after operation: %s", strings.Join(fs.Args(), " "))
	}
	timeout := time.Duration(*timeoutSeconds * float64(time.Second))
	if timeout <= 0 {
		return requestConfig{}, CallOptions{}, "", errors.New("--timeout must be > 0")
	}
	return requestConfig{
		BaseURL: *baseURL,
		Token:   strings.TrimSpace(tokenValue),
		Timeout: timeout,
	}, opts, operationName, nil
}

func (app *App) writePayload(payload any, format string, pretty func() string) error {
	return app.writePayloadTo(app.stdout, payload, format, pretty)
}

func (app *App) writePayloadTo(writer io.Writer, payload any, format string, pretty func() string) error {
	switch format {
	case "json":
		writeJSON(writer, payload)
		return nil
	case "llm":
		writeCompactJSON(writer, payload)
		return nil
	case "pretty":
		_, err := io.WriteString(writer, pretty()+"\n")
		return err
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func (app *App) printRootHelp() {
	lines := append(prettyBrandLines(), "",
		"Official UAPI CLI for discovery, schema inspection, and direct calls.",
		"",
		"Commands:",
		"  discover [query]   Search operations by keyword, tag, or method",
		"  tags               List tags from the embedded OpenAPI index",
		"  schema <op>        Show the normalized contract for one operation",
		"  call <op>          Execute one operation with structured input",
		"",
		"Call input modes:",
		"  --set KEY=VALUE        Auto-route one field using the operation schema",
		"  --path KEY=VALUE       Force a path parameter",
		"  --query KEY=VALUE      Force a query parameter",
		"  --body KEY=VALUE       Force a request-body field",
		"  --header KEY=VALUE     Add a request header",
		"  --file FIELD=PATH      Attach a multipart file field",
		"  --json '{...}'         Send inline JSON body or structured request object",
		"  --json-file FILE       Read JSON body or structured request object from file",
		"  --stdin-json           Read JSON body or structured request object from stdin",
		"",
		"Auth:",
		"  --api-key KEY         Preferred UAPI Key flag",
		"  --token KEY           Compatibility alias for UAPI Key",
		"  --key KEY             Short alias for UAPI Key",
		"  UAPI_TOKEN / UAPI_KEY Environment-variable fallback",
		"",
		"Formats:",
		"  auto = pretty on TTY, llm on non-TTY",
		"  json = indented debug envelope",
		"  llm = compact machine-oriented envelope",
		"  pretty = human-readable terminal output",
		"  raw = raw response body for call",
		"",
		"Examples:",
		"  uapi discover ocr",
		"  uapi schema post-image-ocr --format pretty",
		"  uapi call get-social-bilibili-userinfo --set uid=1945126",
		"  uapi call get-social-bilibili-userinfo --api-key YOUR_UAPI_KEY --set uid=1945126",
		"  uapi call post-image-ocr --body url=https://uapis.cn/ocr-samples/bilingual-poetry-sample.png --body need_location=false",
		"  uapi call post-image-ocr --file file=./demo.png --body image_name=demo.png --body need_location=false",
		"",
		"Console: "+officialConsoleURL,
		"Pricing: "+officialPricingURL,
		"More: "+officialDocsURL,
	)
	_, _ = io.WriteString(app.stdout, strings.Join(lines, "\n")+"\n")
}

func (app *App) printCallHelp() {
	lines := append(prettyBrandLines(), "",
		"uapi call <operation> [flags]",
		"",
		"Purpose:",
		"  Execute one UAPI operation after normalizing path/query/body/header/file inputs.",
		"",
		"Input modes:",
		"  --set KEY=VALUE",
		"    Auto-route a field to path, query, header, or body when the schema makes it unambiguous.",
		"  --path KEY=VALUE",
		"    Set path parameters explicitly, for example task_id=abc123.",
		"  --query KEY=VALUE",
		"    Set query-string parameters explicitly.",
		"  --body KEY=VALUE",
		"    Set request-body fields explicitly. Works for json, form, multipart, and text requests.",
		"  --header KEY=VALUE",
		"    Add request headers explicitly.",
		"  --file FIELD=PATH",
		"    Attach multipart file fields. Use this for binary upload fields such as file=./demo.png.",
		"  --json '{...}' | --json-file FILE | --stdin-json",
		"    Send a full JSON body, or a structured request object with path/query/body/headers/files sections.",
		"",
		"Structured request object:",
		`  {"path":{},"query":{},"body":{},"headers":{},"files":{}}`,
		"",
		"Authentication:",
		"  --api-key KEY | --token KEY | --key KEY",
		"    The CLI sends Authorization: Bearer <KEY> automatically.",
		"  UAPI_TOKEN=KEY | UAPI_KEY=KEY",
		"    Environment-variable fallback for repeated calls.",
		"  --header 'Authorization=Bearer YOUR_UAPI_KEY'",
		"    Manual override when you want to pass the header yourself.",
		"",
		"Useful flags:",
		"  --dry-run            Show the normalized request without executing it",
		"  --disable-cache      Auto-add _t unless you already passed _t yourself",
		"  --cache-buster VAL   Force a specific _t value",
		"  --out FILE           Save binary responses to a chosen path",
		"  --format auto|json|llm|pretty|raw",
		"",
		"Examples:",
		"  uapi call get-social-bilibili-userinfo --set uid=1945126",
		"  uapi call get-social-bilibili-userinfo --api-key YOUR_UAPI_KEY --set uid=1945126",
		"  uapi call get-social-bilibili-userinfo --query uid=1945126 --disable-cache",
		"  uapi call post-image-ocr --body url=https://uapis.cn/ocr-samples/bilingual-poetry-sample.png --body need_location=false",
		"  uapi call post-image-ocr --file file=./demo.png --body image_name=demo.png --body need_location=false",
		"  uapi call post-translate-text --query to_lang=en --json '{\"text\":\"你好\"}'",
		"  $env:UAPI_TOKEN='YOUR_UAPI_KEY'; uapi call post-image-ocr --body url=https://uapis.cn/ocr-samples/bilingual-poetry-sample.png",
		"",
		"Recommended flow:",
		"  1. uapi discover <keyword>",
		"  2. uapi schema <operation>",
		"  3. uapi call <operation> --dry-run",
		"  4. uapi call <operation>",
		"",
		"Official links:",
		"  Console: "+officialConsoleURL,
		"  Pricing: "+officialPricingURL,
		"  Docs:    "+officialDocsURL,
	)
	_, _ = io.WriteString(app.stdout, strings.Join(lines, "\n")+"\n")
}

func newAPIClient(cfg requestConfig) *apiClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &apiClient{
		baseURL: normalizeBaseURL(cfg.BaseURL),
		token:   cfg.Token,
		httpClient: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
	}
}

func (client *apiClient) buildRequest(
	op *specindex.Operation,
	pathParams map[string]any,
	query map[string]any,
	bodyValues map[string]any,
	headers map[string]string,
	fileValues map[string]string,
	jsonPayload any,
	disableCache bool,
	cacheBuster string,
) (*builtRequest, error) {
	path := op.Path
	for _, parameter := range op.ParamsByLocation("path") {
		value, ok := pathParams[parameter.Name]
		if !ok {
			if parameter.Required {
				return nil, fmt.Errorf("missing required path param %q", parameter.Name)
			}
			continue
		}
		path = strings.ReplaceAll(path, "{"+parameter.Name+"}", url.PathEscape(fmt.Sprint(value)))
	}
	queryStrings := make(map[string]string, len(query)+1)
	for key, value := range query {
		queryStrings[key] = stringifyQueryValue(value)
	}
	if cacheBuster != "" {
		queryStrings["_t"] = cacheBuster
	} else if disableCache {
		if _, ok := queryStrings["_t"]; !ok {
			queryStrings["_t"] = strconv.FormatInt(time.Now().UnixMilli(), 10)
		}
	}

	finalURL := client.baseURL + path
	if len(queryStrings) > 0 {
		values := url.Values{}
		keys := make([]string, 0, len(queryStrings))
		for key := range queryStrings {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			values.Set(key, queryStrings[key])
		}
		finalURL += "?" + values.Encode()
	}

	request := &builtRequest{
		Operation:   op,
		OperationID: op.OperationID,
		Method:      op.Method,
		Path:        path,
		URL:         finalURL,
		Headers:     cloneStringMap(headers),
		Query:       queryStrings,
		FileFields:  cloneStringMap(fileValues),
	}
	if op.RequestBody == nil {
		return request, nil
	}
	request.BodyKind = op.RequestBody.Kind()
	switch op.RequestBody.Kind() {
	case "json":
		if jsonPayload != nil {
			request.JSONBody = jsonPayload
		} else if len(bodyValues) > 0 {
			request.JSONBody = bodyValues
		}
	case "text":
		switch typed := jsonPayload.(type) {
		case string:
			request.TextBody = typed
		case nil:
			request.TextBody = deriveTextBody(bodyValues)
		default:
			request.TextBody = compactJSON(typed)
		}
	case "multipart", "form":
		form := make(map[string]string)
		for key, value := range bodyValues {
			form[key] = stringifyFormValue(value)
		}
		request.FormBody = form
	default:
		if jsonPayload != nil {
			request.JSONBody = jsonPayload
		} else if len(bodyValues) > 0 {
			request.JSONBody = bodyValues
		}
	}
	return request, nil
}

func (client *apiClient) execute(ctx context.Context, request *builtRequest, outPath string) (map[string]any, []byte, error) {
	var body io.Reader
	headers := cloneStringMap(request.Headers)
	if headers == nil {
		headers = map[string]string{}
	}
	switch request.BodyKind {
	case "json", "raw":
		if request.JSONBody != nil {
			payload, err := json.Marshal(request.JSONBody)
			if err != nil {
				return nil, nil, err
			}
			body = bytes.NewReader(payload)
			headers["Content-Type"] = "application/json"
		}
	case "text":
		body = strings.NewReader(request.TextBody)
		headers["Content-Type"] = "text/plain"
	case "multipart":
		var payload bytes.Buffer
		writer := multipart.NewWriter(&payload)
		for key, value := range request.FormBody {
			if err := writer.WriteField(key, value); err != nil {
				return nil, nil, err
			}
		}
		fileKeys := make([]string, 0, len(request.FileFields))
		for key := range request.FileFields {
			fileKeys = append(fileKeys, key)
		}
		sort.Strings(fileKeys)
		for _, key := range fileKeys {
			path := request.FileFields[key]
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, nil, fmt.Errorf("read file %q: %w", path, err)
			}
			part, err := writer.CreateFormFile(key, filepath.Base(path))
			if err != nil {
				return nil, nil, err
			}
			if _, err := part.Write(content); err != nil {
				return nil, nil, err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, nil, err
		}
		body = &payload
		headers["Content-Type"] = writer.FormDataContentType()
	case "form":
		values := url.Values{}
		keys := make([]string, 0, len(request.FormBody))
		for key := range request.FormBody {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			values.Set(key, request.FormBody[key])
		}
		payload := values.Encode()
		body = strings.NewReader(payload)
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	}

	httpRequest, err := http.NewRequestWithContext(ctx, request.Method, request.URL, body)
	if err != nil {
		return nil, nil, err
	}
	if client.token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+client.token)
	}
	for key, value := range headers {
		httpRequest.Header.Set(key, value)
	}

	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return nil, nil, err
	}
	defer httpResponse.Body.Close()

	payload, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return nil, nil, err
	}
	meta := extractMetaFromHeaders(httpResponse.Header)
	contentType := strings.TrimSpace(strings.Split(httpResponse.Header.Get("Content-Type"), ";")[0])
	if contentType == "" && len(payload) > 0 {
		contentType = http.DetectContentType(payload)
	}
	if httpResponse.StatusCode >= 400 {
		return nil, nil, mapAPIError(httpResponse.StatusCode, payload, meta)
	}

	response := map[string]any{
		"status":       httpResponse.StatusCode,
		"content_type": contentType,
		"meta":         meta,
	}
	envelope := map[string]any{
		"ok": true,
		"operation": map[string]any{
			"operation_id": request.Operation.OperationID,
			"method":       request.Operation.Method,
			"path":         request.Operation.Path,
			"summary":      request.Operation.Summary,
		},
		"request":  request,
		"response": response,
	}

	if shouldTreatResponseAsBinary(request.Operation, contentType) {
		target, err := saveBinaryResponse(request.Operation.OperationID, contentType, payload, outPath)
		if err != nil {
			return nil, nil, err
		}
		response["binary"] = true
		response["saved_to"] = target
		response["size_bytes"] = len(payload)
		sum := sha256.Sum256(payload)
		response["sha256"] = fmt.Sprintf("%x", sum[:])
		previewBytes := payload
		if len(previewBytes) > 96 {
			previewBytes = previewBytes[:96]
		}
		response["base64_preview"] = base64.StdEncoding.EncodeToString(previewBytes)
		return envelope, payload, nil
	}

	response["binary"] = false
	var parsed any
	if len(payload) > 0 && json.Unmarshal(payload, &parsed) == nil {
		response["data"] = parsed
		return envelope, payload, nil
	}
	response["text"] = string(payload)
	return envelope, payload, nil
}

func collectCallInputs(stdin io.Reader, op *specindex.Operation, opts CallOptions) (map[string]any, map[string]any, map[string]any, map[string]string, map[string]string, any, error) {
	pathValues := map[string]any{}
	queryValues := map[string]any{}
	bodyValues := map[string]any{}
	headerValues := map[string]string{}
	fileValues := map[string]string{}
	var jsonPayload any
	var err error

	if opts.JSONText != "" {
		jsonPayload, err = parseJSONText(opts.JSONText, "--json")
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
	}
	if opts.JSONFile != "" {
		if jsonPayload != nil {
			return nil, nil, nil, nil, nil, nil, errors.New("use only one of --json or --json-file")
		}
		jsonPayload, err = parseJSONFile(opts.JSONFile)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
	}
	if opts.STDINJSON {
		if jsonPayload != nil {
			return nil, nil, nil, nil, nil, nil, errors.New("use stdin JSON or --json/--json-file, not both")
		}
		jsonPayload, err = readJSON(stdin)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
	}

	if structured, ok, err := normalizeStructuredJSON(jsonPayload); err != nil {
		return nil, nil, nil, nil, nil, nil, err
	} else if ok {
		copyIntoAnyMap(pathValues, structured["path"])
		copyIntoAnyMap(queryValues, structured["query"])
		copyIntoAnyMap(bodyValues, structured["body"])
		copyIntoStringMap(headerValues, structured["headers"])
		for key, value := range structured["files"] {
			fileValues[key] = fmt.Sprint(value)
		}
		if err := routeAutoFields(op, structured["auto"], pathValues, queryValues, bodyValues, headerValues, fileValues); err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		jsonPayload = nil
	} else if payloadMap, ok := jsonPayload.(map[string]any); ok && op.RequestBody != nil && (op.RequestBody.Kind() == "multipart" || op.RequestBody.Kind() == "form") {
		for key, value := range payloadMap {
			bodyValues[key] = value
		}
		jsonPayload = nil
	}

	for _, item := range opts.Path {
		key, value, err := parseAssignment(item)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		pathValues[key] = value
	}
	for _, item := range opts.Query {
		key, value, err := parseAssignment(item)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		queryValues[key] = value
	}
	for _, item := range opts.Body {
		key, value, err := parseAssignment(item)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		bodyValues[key] = value
	}
	for _, item := range opts.Header {
		key, value, err := parseStringAssignment(item)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		headerValues[key] = value
	}
	for _, item := range opts.File {
		key, value, err := parseStringAssignment(item)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		fileValues[key] = value
	}
	for _, item := range opts.Set {
		key, value, err := parseAssignment(item)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		if err := routeAutoFields(op, map[string]any{key: value}, pathValues, queryValues, bodyValues, headerValues, fileValues); err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
	}

	return pathValues, queryValues, bodyValues, headerValues, fileValues, jsonPayload, nil
}

func normalizeBaseURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if strings.HasSuffix(value, apiPrefix) {
		value = strings.TrimSuffix(value, apiPrefix)
	}
	if value == "" {
		return defaultBaseURL
	}
	return value
}

func normalizeStructuredJSON(payload any) (map[string]map[string]any, bool, error) {
	input, ok := payload.(map[string]any)
	if !ok || payload == nil {
		return nil, false, nil
	}
	needsStructured := false
	for _, key := range []string{"path", "query", "body", "headers", "files", "json", "payload"} {
		if _, exists := input[key]; exists {
			needsStructured = true
			break
		}
	}
	if !needsStructured {
		return nil, false, nil
	}
	result := map[string]map[string]any{
		"path":    {},
		"query":   {},
		"body":    {},
		"headers": {},
		"files":   {},
		"auto":    {},
	}
	for key, value := range input {
		switch key {
		case "path", "query", "body", "headers", "files":
			object, ok := value.(map[string]any)
			if !ok {
				return nil, false, fmt.Errorf("structured section %q must be an object", key)
			}
			for childKey, childValue := range object {
				result[key][childKey] = childValue
			}
		case "json", "payload":
			if object, ok := value.(map[string]any); ok {
				for childKey, childValue := range object {
					result["body"][childKey] = childValue
				}
			} else {
				result["body"]["_raw"] = value
			}
		default:
			result["auto"][key] = value
		}
	}
	return result, true, nil
}

func routeAutoFields(op *specindex.Operation, source map[string]any, pathValues, queryValues, bodyValues map[string]any, headerValues map[string]string, fileValues map[string]string) error {
	for key, value := range source {
		if key == "_t" {
			queryValues[key] = value
			continue
		}
		locations := op.AllLocationsForName(key)
		if len(locations) == 0 {
			if op.RequestBody != nil && (op.RequestBody.Kind() == "json" || op.RequestBody.Kind() == "multipart" || op.RequestBody.Kind() == "form") {
				bodyValues[key] = value
				continue
			}
			return fmt.Errorf("field %q does not exist in %s", key, op.OperationID)
		}
		if len(locations) > 1 {
			return fmt.Errorf("field %q is ambiguous for %s (%s)", key, op.OperationID, strings.Join(locations, ", "))
		}
		switch locations[0] {
		case "path":
			pathValues[key] = value
		case "query":
			queryValues[key] = value
		case "header":
			headerValues[key] = fmt.Sprint(value)
		case "body":
			if op.RequestBody != nil {
				if field := op.RequestBody.FieldByName(key); field != nil && field.IsBinary() {
					fileValues[key] = fmt.Sprint(value)
					continue
				}
			}
			bodyValues[key] = value
		}
	}
	return nil
}

func parseAssignment(input string) (string, any, error) {
	parts := strings.SplitN(input, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		return "", nil, fmt.Errorf("expected KEY=VALUE, got %q", input)
	}
	return strings.TrimSpace(parts[0]), parseTypedValue(parts[1]), nil
}

func parseStringAssignment(input string) (string, string, error) {
	parts := strings.SplitN(input, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		return "", "", fmt.Errorf("expected KEY=VALUE, got %q", input)
	}
	return strings.TrimSpace(parts[0]), parts[1], nil
}

func parseTypedValue(input string) any {
	value := strings.TrimSpace(input)
	if value == "" {
		return ""
	}
	if value == "true" || value == "false" || value == "null" {
		parsed, err := parseJSONText(value, "inline value")
		if err == nil {
			return parsed
		}
	}
	if isNumber(value) && !hasSuspiciousLeadingZero(value) {
		return json.Number(value)
	}
	if value[0] == '"' || value[0] == '{' || value[0] == '[' {
		parsed, err := parseJSONText(value, "inline value")
		if err == nil {
			return parsed
		}
	}
	return input
}

func isNumber(value string) bool {
	if len(value) == 0 {
		return false
	}
	if value[0] == '-' {
		value = value[1:]
	}
	hasDigit := false
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '.' || r == 'e' || r == 'E' || r == '+' || r == '-':
		default:
			return false
		}
	}
	return hasDigit
}

func hasSuspiciousLeadingZero(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == '-' {
		value = value[1:]
	}
	if len(value) <= 1 {
		return false
	}
	return value[0] == '0' && value[1] >= '0' && value[1] <= '9'
}

func parseJSONText(input, label string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("invalid JSON for %s: %w", label, err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("invalid JSON for %s: trailing content", label)
	}
	return payload, nil
}

func parseJSONFile(path string) (any, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseJSONText(string(payload), path)
}

func readJSON(stdin io.Reader) (any, error) {
	payload, err := io.ReadAll(stdin)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(payload)) == "" {
		return nil, errors.New("stdin is empty; cannot read JSON payload")
	}
	return parseJSONText(string(payload), "stdin")
}

func writeJSON(writer io.Writer, payload any) {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
}

func writeCompactJSON(writer io.Writer, payload any) {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

func prettyJSON(payload any) string {
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func compactJSON(payload any) string {
	data, _ := json.Marshal(payload)
	return string(data)
}

func stringifyQueryValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(value)
	}
}

func stringifyFormValue(value any) string {
	switch typed := value.(type) {
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case json.Number:
		return typed.String()
	case map[string]any, []any:
		return compactJSON(value)
	default:
		return fmt.Sprint(value)
	}
}

func deriveTextBody(values map[string]any) string {
	if len(values) == 1 {
		for _, value := range values {
			if text, ok := value.(string); ok {
				return text
			}
			return compactJSON(value)
		}
	}
	if raw, ok := values["_raw"]; ok {
		if text, ok := raw.(string); ok {
			return text
		}
		return compactJSON(raw)
	}
	if text, ok := values["text"]; ok {
		if typed, ok := text.(string); ok {
			return typed
		}
		return compactJSON(text)
	}
	if len(values) == 0 {
		return ""
	}
	return compactJSON(values)
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

var callFlagsWithValues = map[string]struct{}{
	"base-url":     {},
	"token":        {},
	"api-key":      {},
	"key":          {},
	"timeout":      {},
	"set":          {},
	"path":         {},
	"query":        {},
	"body":         {},
	"header":       {},
	"file":         {},
	"json":         {},
	"json-file":    {},
	"cache-buster": {},
	"out":          {},
	"format":       {},
}

func splitCallArgs(args []string) (string, []string, error) {
	flagArgs := make([]string, 0, len(args))
	operationName := ""
	for index := 0; index < len(args); index++ {
		token := args[index]
		name, hasInlineValue, isFlag := parseFlagToken(token)
		if operationName == "" && !isFlag {
			operationName = token
			continue
		}
		flagArgs = append(flagArgs, token)
		if !isFlag {
			continue
		}
		if _, needsValue := callFlagsWithValues[name]; !needsValue || hasInlineValue {
			continue
		}
		if index+1 >= len(args) {
			return "", nil, fmt.Errorf("flag %q requires a value", token)
		}
		index++
		flagArgs = append(flagArgs, args[index])
	}
	return operationName, flagArgs, nil
}

func splitFirstPositional(args []string) (string, []string) {
	positional := ""
	flagArgs := make([]string, 0, len(args))
	for _, token := range args {
		_, _, isFlag := parseFlagToken(token)
		if positional == "" && !isFlag {
			positional = token
			continue
		}
		flagArgs = append(flagArgs, token)
	}
	return positional, flagArgs
}

func parseFlagToken(token string) (string, bool, bool) {
	if token == "-" || !strings.HasPrefix(token, "-") {
		return "", false, false
	}
	trimmed := strings.TrimLeft(token, "-")
	if trimmed == "" {
		return "", false, false
	}
	if separator := strings.Index(trimmed, "="); separator >= 0 {
		return trimmed[:separator], true, true
	}
	return trimmed, false, true
}

func copyIntoAnyMap(target map[string]any, input map[string]any) {
	for key, value := range input {
		target[key] = value
	}
}

func copyIntoStringMap(target map[string]string, input map[string]any) {
	for key, value := range input {
		target[key] = fmt.Sprint(value)
	}
}

func renderOperationsPretty(ops []*specindex.Operation) string {
	if len(ops) == 0 {
		return strings.Join(append(prettyBrandLines(), "No operations matched."), "\n")
	}
	lines := append(prettyBrandLines(), "")
	for _, op := range ops {
		lines = append(lines, op.OperationID)
		lines = append(lines, fmt.Sprintf("  %s %s  %s", op.Method, op.Path, op.Summary))
		if len(op.Tags) > 0 {
			lines = append(lines, "  tags: "+strings.Join(op.Tags, ", "))
		}
		if len(op.Aliases) > 0 {
			preview := op.Aliases
			if len(preview) > 4 {
				preview = preview[:4]
			}
			lines = append(lines, "  aliases: "+strings.Join(preview, ", "))
		}
	}
	return strings.Join(lines, "\n")
}

func renderTagsPretty(tags []specindex.TagInfo) string {
	if len(tags) == 0 {
		return strings.Join(append(prettyBrandLines(), "No tags found."), "\n")
	}
	lines := append(prettyBrandLines(), "")
	for _, tag := range tags {
		lines = append(lines, tag.Name)
		if tag.Description != "" {
			lines = append(lines, "  "+tag.Description)
		}
	}
	return strings.Join(lines, "\n")
}

func renderSchemaPretty(op *specindex.Operation) string {
	lines := append(prettyBrandLines(), "",
		op.OperationID,
		"method: "+op.Method,
		"path:   "+op.Path,
		"tags:   "+strings.Join(op.Tags, ", "),
	)
	if op.Summary != "" {
		lines = append(lines, "summary: "+op.Summary)
	}
	if op.Description != "" {
		lines = append(lines, "about:   "+op.Description)
	}
	renderParamBlock := func(label string, items []specindex.Parameter) {
		if len(items) == 0 {
			return
		}
		lines = append(lines, label)
		for _, item := range items {
			required := "optional"
			if item.Required {
				required = "required"
			}
			entry := fmt.Sprintf("  - %s (%s, %s)", item.Name, item.Schema.Summary(), required)
			if item.Description != "" {
				entry += " :: " + item.Description
			}
			lines = append(lines, entry)
		}
	}
	renderParamBlock("path params", op.ParamsByLocation("path"))
	renderParamBlock("query params", op.ParamsByLocation("query"))
	renderParamBlock("header params", op.ParamsByLocation("header"))
	if op.RequestBody != nil {
		lines = append(lines, "request body")
		lines = append(lines, "  - kind: "+op.RequestBody.Kind())
		lines = append(lines, "  - content-type: "+op.RequestBody.ContentType)
		lines = append(lines, fmt.Sprintf("  - required: %t", op.RequestBody.Required))
		for _, field := range op.RequestBody.Fields {
			required := "optional"
			if field.Required {
				required = "required"
			}
			entry := fmt.Sprintf("  - %s (%s, %s)", field.Name, field.Schema.Summary(), required)
			if field.Description != "" {
				entry += " :: " + field.Description
			}
			lines = append(lines, entry)
		}
	}
	if len(op.Responses) > 0 {
		lines = append(lines, "responses")
		for _, response := range op.Responses {
			lines = append(lines, "  - "+response.Status)
			if len(response.ContentTypes) > 0 {
				lines = append(lines, "    content-types: "+strings.Join(response.ContentTypes, ", "))
			}
			lines = append(lines, "    body: "+responseBodyLabel(response))
			lines = append(lines, renderResponseSchemaDetails(response.Schema, "    ")...)
			if response.Description != "" {
				lines = append(lines, "    summary: "+response.Description)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func responseBodyLabel(response specindex.Response) string {
	if response.Binary {
		return "binary"
	}
	if response.Schema == nil {
		return "any"
	}
	return response.Schema.Summary()
}

func renderResponseSchemaDetails(schema *specindex.SchemaDigest, indent string) []string {
	if schema == nil {
		return nil
	}
	switch {
	case len(schema.Properties) > 0:
		lines := []string{indent + "fields"}
		return appendSchemaProperties(lines, schema.Properties, indent+"  ")
	case len(schema.OneOf) > 0:
		lines := []string{indent + "variants"}
		return appendSchemaVariants(lines, schema.OneOf, indent+"  ", "variant")
	case len(schema.AnyOf) > 0:
		lines := []string{indent + "variants (anyOf)"}
		return appendSchemaVariants(lines, schema.AnyOf, indent+"  ", "option")
	case len(schema.AllOf) > 0:
		lines := []string{indent + "parts (allOf)"}
		return appendSchemaVariants(lines, schema.AllOf, indent+"  ", "part")
	case schema.Type == "array" && schema.Items != nil:
		return appendSchemaNode([]string{indent + "items"}, indent+"  ", "item", schema.Items, false, false)
	default:
		return nil
	}
}

func appendSchemaProperties(lines []string, properties []specindex.SchemaProperty, indent string) []string {
	for _, property := range properties {
		lines = appendSchemaNode(lines, indent, property.Name, property.Schema, property.Required, false)
	}
	return lines
}

func appendSchemaVariants(lines []string, items []*specindex.SchemaDigest, indent, noun string) []string {
	for index, item := range items {
		label := fmt.Sprintf("%s %d", noun, index+1)
		if item != nil && strings.TrimSpace(item.Title) != "" {
			label += ": " + strings.TrimSpace(item.Title)
		}
		lines = appendSchemaNode(lines, indent, label, item, false, false)
	}
	return lines
}

func appendSchemaNode(lines []string, indent, name string, schema *specindex.SchemaDigest, required, showOptional bool) []string {
	summary := "any"
	if schema != nil {
		summary = schema.Summary()
	}
	entry := fmt.Sprintf("%s- %s (%s", indent, name, summary)
	if required {
		entry += ", required"
	} else if showOptional {
		entry += ", optional"
	}
	entry += ")"
	if schema != nil && schema.Description != "" {
		entry += " :: " + schema.Description
	}
	lines = append(lines, entry)
	if schema == nil {
		return lines
	}
	switch {
	case len(schema.Properties) > 0:
		return appendSchemaProperties(lines, schema.Properties, indent+"  ")
	case schema.Type == "array" && schema.Items != nil:
		return appendSchemaNode(lines, indent+"  ", "item", schema.Items, false, false)
	case len(schema.OneOf) > 0:
		return appendSchemaVariants(lines, schema.OneOf, indent+"  ", "variant")
	case len(schema.AnyOf) > 0:
		return appendSchemaVariants(lines, schema.AnyOf, indent+"  ", "option")
	case len(schema.AllOf) > 0:
		return appendSchemaVariants(lines, schema.AllOf, indent+"  ", "part")
	default:
		return lines
	}
}

func prettyBrandLines() []string {
	return []string{
		officialASCII,
		"https://" + officialHost,
	}
}

func renderCallPretty(envelope map[string]any) string {
	response, _ := envelope["response"].(map[string]any)
	lines := []string{
		"request ok",
		fmt.Sprintf("operation: %v", envelope["operation"].(map[string]any)["operation_id"]),
		fmt.Sprintf("status:    %v", response["status"]),
		fmt.Sprintf("type:      %v", response["content_type"]),
	}
	if meta, ok := response["meta"].(responseMeta); ok && meta.RequestID != "" {
		lines = append(lines, "request-id: "+meta.RequestID)
	}
	if binary, _ := response["binary"].(bool); binary {
		lines = append(lines, fmt.Sprintf("saved:     %v", response["saved_to"]))
		lines = append(lines, fmt.Sprintf("size:      %v bytes", response["size_bytes"]))
		lines = append(lines, fmt.Sprintf("sha256:    %v", response["sha256"]))
		return strings.Join(lines, "\n")
	}
	if data, ok := response["data"]; ok {
		lines = append(lines, "data")
		lines = append(lines, prettyJSON(data))
		return strings.Join(lines, "\n")
	}
	if text, ok := response["text"].(string); ok {
		lines = append(lines, "text")
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

func discoverPayload(query, tag, method string, ops []*specindex.Operation) map[string]any {
	items := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		items = append(items, map[string]any{
			"operation_id": op.OperationID,
			"method":       op.Method,
			"path":         op.Path,
			"summary":      op.Summary,
			"tags":         op.Tags,
		})
	}
	payload := map[string]any{
		"ok":    true,
		"count": len(items),
		"items": items,
	}
	filters := map[string]any{}
	if strings.TrimSpace(query) != "" {
		filters["query"] = query
	}
	if strings.TrimSpace(tag) != "" {
		filters["tag"] = tag
	}
	if strings.TrimSpace(method) != "" {
		filters["method"] = strings.ToUpper(strings.TrimSpace(method))
	}
	if len(filters) > 0 {
		payload["filters"] = filters
	}
	return payload
}

func tagsPayload(tags []specindex.TagInfo) map[string]any {
	items := make([]map[string]any, 0, len(tags))
	for _, tag := range tags {
		entry := map[string]any{"name": tag.Name}
		if tag.Description != "" {
			entry["description"] = tag.Description
		}
		items = append(items, entry)
	}
	return map[string]any{
		"ok":    true,
		"count": len(items),
		"items": items,
	}
}

func schemaPayload(op *specindex.Operation) map[string]any {
	if op == nil {
		return nil
	}
	payload := map[string]any{
		"ok":           true,
		"operation_id": op.OperationID,
		"method":       op.Method,
		"path":         op.Path,
		"tags":         op.Tags,
	}
	if op.Summary != "" {
		payload["summary"] = op.Summary
	}
	if op.Description != "" {
		payload["description"] = op.Description
	}
	if len(op.Parameters) > 0 {
		payload["parameters"] = op.Parameters
	}
	if op.RequestBody != nil {
		payload["request_body"] = op.RequestBody
	}
	if len(op.Responses) > 0 {
		payload["responses"] = op.Responses
	}
	return payload
}

func dryRunPayload(request *builtRequest) map[string]any {
	return map[string]any{
		"ok":      true,
		"dry_run": true,
		"request": requestForOutput(request),
	}
}

func callPayload(envelope map[string]any) map[string]any {
	payload := map[string]any{
		"ok": true,
	}
	if operation, ok := envelope["operation"].(map[string]any); ok {
		payload["operation"] = operation
	}
	if request, ok := envelope["request"].(*builtRequest); ok {
		payload["request"] = requestForOutput(request)
	}
	if response, ok := envelope["response"].(map[string]any); ok {
		payload["response"] = responseForOutput(response, true)
	}
	return payload
}

func requestForOutput(request *builtRequest) map[string]any {
	if request == nil {
		return nil
	}
	payload := map[string]any{
		"operation_id": request.OperationID,
		"method":       request.Method,
		"path":         request.Path,
		"url":          request.URL,
	}
	if len(request.Query) > 0 {
		payload["query"] = request.Query
	}
	if len(request.Headers) > 0 {
		payload["headers"] = request.Headers
	}
	if request.BodyKind != "" {
		payload["body_kind"] = request.BodyKind
	}
	if request.JSONBody != nil {
		payload["json_body"] = request.JSONBody
	}
	if request.TextBody != "" {
		payload["text_body"] = request.TextBody
	}
	if len(request.FormBody) > 0 {
		payload["form_body"] = request.FormBody
	}
	if len(request.FileFields) > 0 {
		payload["file_fields"] = request.FileFields
	}
	return payload
}

func responseForOutput(response map[string]any, compact bool) map[string]any {
	if response == nil {
		return nil
	}
	payload := map[string]any{
		"status":       response["status"],
		"content_type": response["content_type"],
	}
	if binary, ok := response["binary"].(bool); ok {
		payload["binary"] = binary
	}
	if meta, ok := response["meta"].(responseMeta); ok {
		payload["meta"] = metaForOutput(meta, compact)
	}
	for _, key := range []string{"saved_to", "size_bytes", "sha256", "base64_preview", "data", "text"} {
		if value, ok := response[key]; ok {
			payload[key] = value
		}
	}
	return payload
}

func metaForOutput(meta responseMeta, compact bool) map[string]any {
	payload := map[string]any{}
	if meta.RequestID != "" {
		payload["request_id"] = meta.RequestID
	}
	if meta.DebitStatus != "" {
		payload["debit_status"] = meta.DebitStatus
	}
	if meta.CreditsCharged != nil {
		payload["credits_charged"] = *meta.CreditsCharged
	}
	if meta.CreditsRequested != nil {
		payload["credits_requested"] = *meta.CreditsRequested
	}
	if meta.RetryAfterSeconds != nil {
		payload["retry_after_seconds"] = *meta.RetryAfterSeconds
	}
	if meta.ActiveQuotaBuckets != nil {
		payload["active_quota_buckets"] = *meta.ActiveQuotaBuckets
	}
	if meta.StopOnEmpty != nil {
		payload["stop_on_empty"] = *meta.StopOnEmpty
	}
	if meta.BalanceLimitCents != nil {
		payload["balance_limit_cents"] = *meta.BalanceLimitCents
	}
	if meta.BalanceRemainingCents != nil {
		payload["balance_remaining_cents"] = *meta.BalanceRemainingCents
	}
	if meta.QuotaLimitCredits != nil {
		payload["quota_limit_credits"] = *meta.QuotaLimitCredits
	}
	if meta.QuotaRemainingCredits != nil {
		payload["quota_remaining_credits"] = *meta.QuotaRemainingCredits
	}
	if meta.VisitorQuotaLimitCredits != nil {
		payload["visitor_quota_limit_credits"] = *meta.VisitorQuotaLimitCredits
	}
	if meta.VisitorQuotaRemainingCredits != nil {
		payload["visitor_quota_remaining_credits"] = *meta.VisitorQuotaRemainingCredits
	}
	if !compact {
		if meta.CreditsPricing != "" {
			payload["credits_pricing"] = meta.CreditsPricing
		}
		if meta.RateLimitPolicyRaw != "" {
			payload["rate_limit_policy_raw"] = meta.RateLimitPolicyRaw
		}
		if meta.RateLimitRaw != "" {
			payload["rate_limit_raw"] = meta.RateLimitRaw
		}
		if len(meta.RateLimitPolicies) > 0 {
			payload["rate_limit_policies"] = meta.RateLimitPolicies
		}
		if len(meta.RateLimits) > 0 {
			payload["rate_limits"] = meta.RateLimits
		}
		if len(meta.RawHeaders) > 0 {
			payload["raw_headers"] = meta.RawHeaders
		}
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func apiErrorForOutput(apiErr *apiError, compact bool, operationName string) map[string]any {
	if apiErr == nil {
		return nil
	}
	payload := map[string]any{
		"code":    apiErr.Code,
		"status":  apiErr.Status,
		"message": apiErr.Message,
	}
	if apiErr.Details != nil {
		payload["details"] = apiErr.Details
	}
	if !compact && apiErr.Payload != nil {
		payload["payload"] = apiErr.Payload
	}
	if meta := metaForOutput(apiErr.Meta, compact); meta != nil {
		payload["meta"] = meta
	}
	if guidance := buildAPIErrorGuidance(apiErr, operationName); guidance != nil {
		payload["kind"] = guidance.Kind
		payload["hint"] = guidance.Summary
		if !compact && guidance.Title != "" {
			payload["title"] = guidance.Title
		}
		if len(guidance.Actions) > 0 {
			payload["actions"] = guidance.Actions
		}
	}
	return payload
}

type commandMode string

const (
	commandModeDiscover commandMode = "discover"
	commandModeTags     commandMode = "tags"
	commandModeSchema   commandMode = "schema"
	commandModeCall     commandMode = "call"
)

func resolveFormat(input string, mode commandMode, writer io.Writer) string {
	if input == "" || input == "auto" {
		switch mode {
		case commandModeDiscover, commandModeTags, commandModeSchema:
			if isTTY(writer) {
				return "pretty"
			}
			return "llm"
		case commandModeCall:
			if isTTY(writer) {
				return "pretty"
			}
			return "llm"
		}
	}
	return input
}

func errorOutputFormat(format string) string {
	if format == "llm" {
		return "llm"
	}
	if format == "pretty" {
		return "pretty"
	}
	return "json"
}

func defaultTokenValue() string {
	for _, key := range []string{"UAPI_TOKEN", "UAPI_KEY"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func isTTY(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func getenvDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getenvFloat(key string, fallback float64) float64 {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func extractMetaFromHeaders(headers http.Header) responseMeta {
	rawHeaders := map[string]string{}
	for key, values := range headers {
		if len(values) == 0 {
			continue
		}
		rawHeaders[strings.ToLower(key)] = strings.Join(values, ", ")
	}
	policies := parseStructuredItems(rawHeaders["ratelimit-policy"])
	limits := parseStructuredItems(rawHeaders["ratelimit"])
	meta := responseMeta{
		RequestID:          rawHeaders["x-request-id"],
		DebitStatus:        rawHeaders["uapi-debit-status"],
		CreditsPricing:     rawHeaders["uapi-credits-pricing"],
		RateLimitPolicyRaw: rawHeaders["ratelimit-policy"],
		RateLimitRaw:       rawHeaders["ratelimit"],
		RateLimitPolicies:  map[string]rateLimitPolicyEntry{},
		RateLimits:         map[string]rateLimitStateEntry{},
		RawHeaders:         rawHeaders,
	}
	meta.RetryAfterSeconds = parseIntPtr(rawHeaders["retry-after"])
	meta.CreditsRequested = parseIntPtr(rawHeaders["uapi-credits-requested"])
	meta.CreditsCharged = parseIntPtr(rawHeaders["uapi-credits-charged"])
	meta.ActiveQuotaBuckets = parseIntPtr(rawHeaders["uapi-quota-active-buckets"])
	meta.StopOnEmpty = parseBoolPtr(rawHeaders["uapi-stop-on-empty"])
	for _, item := range policies {
		entry := rateLimitPolicyEntry{
			Name:          item.Name,
			Quota:         parseIntPtr(item.Params["q"]),
			Unit:          item.Params["uapi-unit"],
			WindowSeconds: parseIntPtr(item.Params["w"]),
		}
		meta.RateLimitPolicies[item.Name] = entry
	}
	for _, item := range limits {
		entry := rateLimitStateEntry{
			Name:              item.Name,
			Remaining:         parseIntPtr(item.Params["r"]),
			Unit:              item.Params["uapi-unit"],
			ResetAfterSeconds: parseIntPtr(item.Params["t"]),
		}
		meta.RateLimits[item.Name] = entry
	}
	if entry, ok := meta.RateLimitPolicies["billing-balance"]; ok {
		meta.BalanceLimitCents = entry.Quota
	}
	if entry, ok := meta.RateLimits["billing-balance"]; ok {
		meta.BalanceRemainingCents = entry.Remaining
	}
	if entry, ok := meta.RateLimitPolicies["billing-quota"]; ok {
		meta.QuotaLimitCredits = entry.Quota
	}
	if entry, ok := meta.RateLimits["billing-quota"]; ok {
		meta.QuotaRemainingCredits = entry.Remaining
	}
	if entry, ok := meta.RateLimitPolicies["visitor-quota"]; ok {
		meta.VisitorQuotaLimitCredits = entry.Quota
	}
	if entry, ok := meta.RateLimits["visitor-quota"]; ok {
		meta.VisitorQuotaRemainingCredits = entry.Remaining
	}
	return meta
}

type structuredItem struct {
	Name   string
	Params map[string]string
}

func parseStructuredItems(raw string) []structuredItem {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]structuredItem, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments := strings.Split(part, ";")
		item := structuredItem{Name: strings.Trim(strings.TrimSpace(segments[0]), "\""), Params: map[string]string{}}
		for _, segment := range segments[1:] {
			kv := strings.SplitN(strings.TrimSpace(segment), "=", 2)
			if len(kv) == 2 {
				item.Params[kv[0]] = strings.Trim(kv[1], "\"")
			}
		}
		out = append(out, item)
	}
	return out
}

func parseIntPtr(value string) *int64 {
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func parseBoolPtr(value string) *bool {
	if value == "" {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "true":
		v := true
		return &v
	case "false":
		v := false
		return &v
	default:
		return nil
	}
}

func mapAPIError(status int, payload []byte, meta responseMeta) error {
	bodyText := strings.TrimSpace(string(payload))
	var parsed map[string]any
	_ = json.Unmarshal(payload, &parsed)
	message := extractErrorMessage(parsed, bodyText, status)
	code := normalizeAPIErrorCode(extractErrorCode(parsed, status), status, message, meta)
	return &apiError{
		Code:    code,
		Status:  status,
		Message: message,
		Details: firstNonNil(parsed["details"], parsed["errors"], parsed["quota"], parsed["docs"]),
		Payload: parsed,
		Meta:    meta,
	}
}

func extractErrorCode(parsed map[string]any, status int) string {
	code := stringOrDefault(parsed["code"], "")
	if code != "" {
		return strings.ToUpper(code)
	}
	errorValue := stringOrDefault(parsed["error"], "")
	if looksLikeMachineCode(errorValue) {
		return strings.ToUpper(errorValue)
	}
	return defaultErrorCode(status)
}

func extractErrorMessage(parsed map[string]any, bodyText string, status int) string {
	for _, key := range []string{"message", "msg", "detail", "error_description"} {
		if text := stringOrDefault(parsed[key], ""); text != "" {
			return text
		}
	}
	if errorText := stringOrDefault(parsed["error"], ""); errorText != "" && !looksLikeMachineCode(errorText) {
		return errorText
	}
	if bodyText != "" && !looksLikeJSON(bodyText) {
		return bodyText
	}
	if statusText := http.StatusText(status); statusText != "" {
		return statusText
	}
	if bodyText != "" {
		return bodyText
	}
	return defaultErrorCode(status)
}

func looksLikeMachineCode(value string) bool {
	if value == "" {
		return false
	}
	hasLetter := false
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= 'a' && r <= 'z':
			hasLetter = true
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == ':':
		default:
			return false
		}
	}
	return hasLetter && value == strings.ToUpper(value)
}

func looksLikeJSON(value string) bool {
	if value == "" {
		return false
	}
	switch value[0] {
	case '{', '[':
		return true
	default:
		return false
	}
}

func defaultErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "INVALID_PARAMETER"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusPaymentRequired:
		return "INSUFFICIENT_CREDITS"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusRequestEntityTooLarge:
		return "REQUEST_ENTITY_TOO_LARGE"
	case http.StatusTooManyRequests:
		return "SERVICE_BUSY"
	case http.StatusInternalServerError:
		return "INTERNAL_SERVER_ERROR"
	default:
		return "API_ERROR"
	}
}

func normalizeAPIErrorCode(code string, status int, message string, meta responseMeta) string {
	normalized := strings.ToUpper(strings.TrimSpace(code))
	if normalized == "" {
		normalized = defaultErrorCode(status)
	}
	if status == http.StatusTooManyRequests {
		if normalized == "VISITOR_MONTHLY_QUOTA_EXHAUSTED" || visitorQuotaMetaExhausted(meta) || containsVisitorQuotaText(message) {
			return "VISITOR_MONTHLY_QUOTA_EXHAUSTED"
		}
	}
	if status == http.StatusPaymentRequired {
		return "INSUFFICIENT_CREDITS"
	}
	if status == http.StatusUnauthorized {
		return "UNAUTHORIZED"
	}
	return normalized
}

func buildAPIErrorGuidance(apiErr *apiError, operationName string) *apiErrorGuidance {
	if apiErr == nil {
		return nil
	}
	switch {
	case isVisitorQuotaExhausted(apiErr):
		return &apiErrorGuidance{
			Kind:    "visitor_quota_exhausted",
			Title:   "Visitor free quota exhausted",
			Summary: "Anonymous access has used up its free monthly quota. Create a free UAPI Key, then retry with `--api-key` or `UAPI_TOKEN`.",
			Actions: append(commonAuthActions(operationName), apiErrorAction{
				Label:       "View pricing and credit packs",
				Description: "Check paid credit packs when you need more stable or higher-volume usage.",
				URL:         officialPricingURL,
			}),
		}
	case isInsufficientCredits(apiErr):
		return &apiErrorGuidance{
			Kind:    "insufficient_credits",
			Title:   "Insufficient balance or credits",
			Summary: "The current UAPI Key does not have enough balance or credits for this request. Recharge, or switch to another key with remaining quota.",
			Actions: []apiErrorAction{
				{
					Label:       "Open pricing and recharge",
					Description: "View credit packs and pricing for the current account.",
					URL:         officialPricingURL,
				},
				{
					Label:       "Manage or switch UAPI Keys",
					Description: "Create, inspect, or rotate keys in the UAPI Console.",
					URL:         officialConsoleURL,
				},
				{
					Label:       "Retry with another key",
					Description: "Pass a different key directly for this call.",
					Example:     callAuthExample(operationName),
				},
				{
					Label:       "Set a default key in PowerShell",
					Description: "The CLI also accepts UAPI_KEY as an environment variable.",
					Example:     "$env:UAPI_TOKEN='YOUR_UAPI_KEY'",
				},
			},
		}
	case isUnauthorized(apiErr):
		return &apiErrorGuidance{
			Kind:    "unauthorized",
			Title:   "Missing or invalid UAPI Key",
			Summary: "This request needs a valid UAPI Key, or the provided key is invalid or expired. Create or inspect your key in the console, then retry.",
			Actions: commonAuthActions(operationName),
		}
	case apiErr.Status == http.StatusTooManyRequests:
		summary := "The upstream service is rate-limiting or temporarily busy. Retry later, or use a UAPI Key for more stable access."
		if apiErr.Meta.RetryAfterSeconds != nil {
			summary = fmt.Sprintf("The upstream service is rate-limiting or temporarily busy. Retry after %d seconds, or use a UAPI Key for more stable access.", *apiErr.Meta.RetryAfterSeconds)
		}
		return &apiErrorGuidance{
			Kind:    "service_busy",
			Title:   "Rate limit or temporary service pressure",
			Summary: summary,
			Actions: []apiErrorAction{
				{
					Label:       "Create or manage a UAPI Key",
					Description: "Authenticated calls usually have better quota visibility than anonymous traffic.",
					URL:         officialConsoleURL,
				},
				{
					Label:       "View pricing and credit packs",
					Description: "Use a paid key if you need steadier throughput.",
					URL:         officialPricingURL,
				},
			},
		}
	default:
		return nil
	}
}

func commonAuthActions(operationName string) []apiErrorAction {
	return []apiErrorAction{
		{
			Label:       "Create or manage a UAPI Key",
			Description: "Open the official UAPI Console.",
			URL:         officialConsoleURL,
		},
		{
			Label:       "Pass the key directly",
			Description: "Accepted flags: --api-key, --token, --key.",
			Example:     callAuthExample(operationName),
		},
		{
			Label:       "Set a default key in PowerShell",
			Description: "The CLI accepts both UAPI_TOKEN and UAPI_KEY.",
			Example:     "$env:UAPI_TOKEN='YOUR_UAPI_KEY'",
		},
	}
}

func callAuthExample(operationName string) string {
	name := strings.TrimSpace(operationName)
	if name == "" {
		name = "<operation>"
	}
	return fmt.Sprintf("uapi call %s --api-key YOUR_UAPI_KEY ...", name)
}

func isVisitorQuotaExhausted(apiErr *apiError) bool {
	if apiErr == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(apiErr.Code), "VISITOR_MONTHLY_QUOTA_EXHAUSTED") {
		return true
	}
	if apiErr.Status != http.StatusTooManyRequests {
		return false
	}
	return visitorQuotaMetaExhausted(apiErr.Meta) || containsVisitorQuotaText(apiErr.Message)
}

func isInsufficientCredits(apiErr *apiError) bool {
	if apiErr == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(apiErr.Code), "INSUFFICIENT_CREDITS") {
		return true
	}
	return apiErr.Status == http.StatusPaymentRequired
}

func isUnauthorized(apiErr *apiError) bool {
	if apiErr == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(apiErr.Code), "UNAUTHORIZED") {
		return true
	}
	return apiErr.Status == http.StatusUnauthorized
}

func visitorQuotaMetaExhausted(meta responseMeta) bool {
	return meta.VisitorQuotaRemainingCredits != nil && *meta.VisitorQuotaRemainingCredits <= 0
}

func containsVisitorQuotaText(message string) bool {
	return containsAnyFold(message,
		"visitor monthly quota",
		"visitor quota",
		"anonymous quota",
		"anonymous access",
		"访客",
		"匿名",
		"免费额度",
		"免费积分",
	)
}

func containsAnyFold(value string, needles ...string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}
	for _, needle := range needles {
		if needle != "" && strings.Contains(normalized, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func renderAPIErrorPretty(operationName string, apiErr *apiError) string {
	lines := append(prettyBrandLines(), "", "request failed")
	if operationName != "" {
		lines = append(lines, "operation: "+operationName)
	}
	lines = append(lines,
		fmt.Sprintf("status:    %d", apiErr.Status),
		"code:      "+apiErr.Code,
		"message:   "+apiErr.Message,
	)
	if apiErr.Meta.RequestID != "" {
		lines = append(lines, "request-id: "+apiErr.Meta.RequestID)
	}
	if guidance := buildAPIErrorGuidance(apiErr, operationName); guidance != nil {
		if guidance.Title != "" {
			lines = append(lines, "reason:    "+guidance.Title)
		}
		if guidance.Summary != "" {
			lines = append(lines, "hint:      "+guidance.Summary)
		}
		if facts := describeErrorMetaFacts(apiErr.Meta); len(facts) > 0 {
			lines = append(lines, "facts")
			for _, fact := range facts {
				lines = append(lines, "  - "+fact)
			}
		}
		if len(guidance.Actions) > 0 {
			lines = append(lines, "next")
			for _, action := range guidance.Actions {
				entry := "  - " + action.Label
				if action.Description != "" {
					entry += ": " + action.Description
				}
				lines = append(lines, entry)
				if action.URL != "" {
					lines = append(lines, "    "+action.URL)
				}
				if action.Example != "" {
					lines = append(lines, "    "+action.Example)
				}
			}
		}
		return strings.Join(lines, "\n")
	}
	if facts := describeErrorMetaFacts(apiErr.Meta); len(facts) > 0 {
		lines = append(lines, "facts")
		for _, fact := range facts {
			lines = append(lines, "  - "+fact)
		}
	}
	return strings.Join(lines, "\n")
}

func renderCLIErrorPretty(err error) string {
	lines := append(prettyBrandLines(), "",
		"cli error",
		"message: "+err.Error(),
		"docs:    "+officialDocsURL,
	)
	return strings.Join(lines, "\n")
}

func describeErrorMetaFacts(meta responseMeta) []string {
	facts := []string{}
	if meta.CreditsRequested != nil {
		facts = append(facts, fmt.Sprintf("credits requested: %d", *meta.CreditsRequested))
	}
	if meta.CreditsCharged != nil {
		facts = append(facts, fmt.Sprintf("credits charged: %d", *meta.CreditsCharged))
	}
	if meta.CreditsPricing != "" {
		facts = append(facts, "credits pricing: "+meta.CreditsPricing)
	}
	if meta.VisitorQuotaRemainingCredits != nil {
		facts = append(facts, fmt.Sprintf("visitor quota remaining: %d credits", *meta.VisitorQuotaRemainingCredits))
	}
	if meta.VisitorQuotaLimitCredits != nil {
		facts = append(facts, fmt.Sprintf("visitor quota limit: %d credits", *meta.VisitorQuotaLimitCredits))
	}
	if meta.QuotaRemainingCredits != nil {
		facts = append(facts, fmt.Sprintf("quota remaining: %d credits", *meta.QuotaRemainingCredits))
	}
	if meta.QuotaLimitCredits != nil {
		facts = append(facts, fmt.Sprintf("quota limit: %d credits", *meta.QuotaLimitCredits))
	}
	if meta.BalanceRemainingCents != nil {
		facts = append(facts, "balance remaining: "+formatCents(*meta.BalanceRemainingCents))
	}
	if meta.BalanceLimitCents != nil {
		facts = append(facts, "balance limit: "+formatCents(*meta.BalanceLimitCents))
	}
	if meta.RetryAfterSeconds != nil {
		facts = append(facts, fmt.Sprintf("retry after: %d seconds", *meta.RetryAfterSeconds))
	}
	return facts
}

func formatCents(cents int64) string {
	return fmt.Sprintf("¥%.2f (%d cents)", float64(cents)/100, cents)
}

func stringOrDefault(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return fallback
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func isBinaryContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	return strings.HasPrefix(contentType, "image/") ||
		strings.HasPrefix(contentType, "audio/") ||
		strings.HasPrefix(contentType, "video/") ||
		contentType == "application/octet-stream" ||
		contentType == "application/pdf" ||
		contentType == "application/zip"
}

func shouldTreatResponseAsBinary(op *specindex.Operation, contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case contentType == "":
		return responseDeclaresBinary(op, "")
	case isTextualContentType(contentType):
		return false
	case isBinaryContentType(contentType):
		return true
	default:
		return responseDeclaresBinary(op, contentType)
	}
}

func responseDeclaresBinary(op *specindex.Operation, contentType string) bool {
	if op == nil {
		return false
	}
	for _, response := range op.Responses {
		if response.Binary {
			if len(response.ContentTypes) == 0 {
				return true
			}
			for _, candidate := range response.ContentTypes {
				if strings.EqualFold(candidate, contentType) {
					return true
				}
			}
		}
	}
	return false
}

func isTextualContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	return strings.HasPrefix(contentType, "text/") ||
		contentType == "application/json" ||
		contentType == "application/problem+json" ||
		contentType == "application/xml" ||
		contentType == "application/x-www-form-urlencoded" ||
		strings.HasSuffix(contentType, "+json") ||
		strings.HasSuffix(contentType, "+xml")
}

func saveBinaryResponse(operationID, contentType string, payload []byte, outPath string) (string, error) {
	target := outPath
	if target == "" {
		target = filepath.Join(os.TempDir(), "uapi-"+operationID+guessExtension(contentType))
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, payload, 0o644); err != nil {
		return "", err
	}
	return target, nil
}

func guessExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "application/json":
		return ".json"
	case "text/plain":
		return ".txt"
	default:
		return ".bin"
	}
}

type errSilent struct{}

func (errSilent) Error() string { return "" }
