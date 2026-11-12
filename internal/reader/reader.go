// Package reader provides the single OpenAI-compatible chat completion client
// for the semguard lab, with no retries and no tools.
package reader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// DefaultMaxResponseBytes bounds response bodies when Client.MaxResponseBytes is zero (1 MiB).
const DefaultMaxResponseBytes = 1 << 20

// DefaultMaxRequestBytes bounds request bodies when Client.MaxRequestBytes is zero (512 KiB).
const DefaultMaxRequestBytes = 512 << 10

// Mode selects how the reader requests output from the chat endpoint.
type Mode string

const (
	// ModeFree requests an unconstrained plain chat completion.
	ModeFree Mode = "free"
	// ModeSchema forces output adherence via strict response_format: json_schema.
	ModeSchema Mode = "schema"
)

// Client calls an OpenAI-compatible /chat/completions endpoint.
type Client struct {
	BaseURL          string
	APIKey           string
	HTTPClient       *http.Client
	MaxResponseBytes int64
	MaxRequestBytes  int64
}

// Request is one reader completion request.
type Request struct {
	Model      string
	System     string
	User       string
	Mode       Mode
	TaskID     string
	SchemaJSON []byte
}

// Response is the result of one reader completion call.
type Response struct {
	Content      string
	RawBody      []byte
	Refused      bool
	Refusal      string
	FinishReason string
	SchemaSHA256 string
}

// StatusError is a non-2xx HTTP response from the chat endpoint.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string { return fmt.Sprintf("reader: HTTP %d: %s", e.Code, e.Body) }

// Transient reports whether a Complete error may clear on a later attempt: a request timeout, a connection error,
// HTTP 429, or HTTP 5xx. A malformed 2xx body, any other 4xx, and request-building errors are not transient.
// Complete itself never retries; the caller decides, and must not retry once its own context is done.
func Transient(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code == http.StatusTooManyRequests || se.Code >= 500
	}
	var ue *url.Error
	var ne net.Error
	return errors.As(err, &ue) || errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded)
}

// LoadSchema reads a task JSON schema file, verifies it is valid JSON, and returns its raw bytes and hex SHA-256.
func LoadSchema(path string) ([]byte, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, DefaultMaxRequestBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > DefaultMaxRequestBytes {
		return nil, "", fmt.Errorf("%s: larger than %d bytes", path, DefaultMaxRequestBytes)
	}
	var js json.RawMessage
	if err := json.Unmarshal(b, &js); err != nil {
		return nil, "", fmt.Errorf("parse schema %s: %w", path, err)
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), nil
}

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type wireJSONSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type wireResponseFormat struct {
	Type       string         `json:"type"`
	JSONSchema wireJSONSchema `json:"json_schema"`
}

type wireRequest struct {
	Model          string              `json:"model"`
	Messages       []wireMessage       `json:"messages"`
	Temperature    float64             `json:"temperature"`
	ResponseFormat *wireResponseFormat `json:"response_format,omitempty"`
}

type wireResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content *string `json:"content"`
			Refusal *string `json:"refusal"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete sends one chat completion request with no retries and no tools.
func (c *Client) Complete(ctx context.Context, req Request) (Response, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return Response{}, errors.New("reader: BaseURL is empty")
	}
	if strings.TrimSpace(req.Model) == "" {
		return Response{}, errors.New("reader: Model is empty")
	}
	mode := req.Mode
	if mode == "" {
		mode = ModeFree
	}
	if mode != ModeFree && mode != ModeSchema {
		return Response{}, fmt.Errorf("reader: unknown mode %q", mode)
	}

	var msgs []wireMessage
	if req.System != "" {
		msgs = append(msgs, wireMessage{Role: "system", Content: req.System})
	}
	msgs = append(msgs, wireMessage{Role: "user", Content: req.User})

	wreq := wireRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: 0,
	}

	var schemaSHA string
	if mode == ModeSchema {
		if len(req.SchemaJSON) == 0 {
			return Response{}, errors.New("reader: schema mode requires SchemaJSON")
		}
		var rawSchema json.RawMessage
		if err := json.Unmarshal(req.SchemaJSON, &rawSchema); err != nil {
			return Response{}, fmt.Errorf("reader: invalid SchemaJSON: %w", err)
		}
		name := req.TaskID
		if name == "" {
			name = "task_output"
		}
		sum := sha256.Sum256(req.SchemaJSON)
		schemaSHA = hex.EncodeToString(sum[:])
		wreq.ResponseFormat = &wireResponseFormat{
			Type: "json_schema",
			JSONSchema: wireJSONSchema{
				Name:   name,
				Strict: true,
				Schema: rawSchema,
			},
		}
	}

	payload, err := json.Marshal(wreq)
	if err != nil {
		return Response{}, fmt.Errorf("reader: marshal request: %w", err)
	}
	maxReq := c.MaxRequestBytes
	if maxReq <= 0 {
		maxReq = DefaultMaxRequestBytes
	}
	if int64(len(payload)) > maxReq {
		return Response{}, fmt.Errorf("reader: request body %d bytes exceeds max %d", len(payload), maxReq)
	}

	endpoint := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("reader: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	httpResp, err := hc.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("reader: transport: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	maxResp := c.MaxResponseBytes
	if maxResp <= 0 {
		maxResp = DefaultMaxResponseBytes
	}
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResp+1))
	if err != nil {
		return Response{}, fmt.Errorf("reader: read response: %w", err)
	}
	if int64(len(body)) > maxResp {
		return Response{}, fmt.Errorf("reader: response body exceeds max %d bytes", maxResp)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return Response{}, &StatusError{Code: httpResp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	var wresp wireResponse
	if err := json.Unmarshal(body, &wresp); err != nil {
		return Response{}, fmt.Errorf("reader: decode response: %w", err)
	}
	if len(wresp.Choices) == 0 {
		return Response{}, errors.New("reader: response contains no choices")
	}

	ch := wresp.Choices[0]
	var content, refusal string
	if ch.Message.Content != nil {
		content = *ch.Message.Content
	}
	if ch.Message.Refusal != nil {
		refusal = strings.TrimSpace(*ch.Message.Refusal)
	}
	refused := refusal != "" || ch.FinishReason == "content_filter" || (ch.FinishReason == "refusal")
	if !refused && strings.TrimSpace(content) == "" {
		return Response{}, errors.New("reader: empty assistant content without refusal")
	}

	return Response{
		Content:      content,
		RawBody:      body,
		Refused:      refused,
		Refusal:      refusal,
		FinishReason: ch.FinishReason,
		SchemaSHA256: schemaSHA,
	}, nil
}
