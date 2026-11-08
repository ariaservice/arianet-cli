package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CloudPrefix is where the API gateway serves the cloud API: it proxies
// <api_url>/cloud/v1/<path> to the cloud service unchanged.
const CloudPrefix = "/cloud/v1"

const (
	// cloudReadRetries is how many extra attempts a read gets after a
	// transport error, a 502/503 or a 429.
	cloudReadRetries = 2
	// maxRateLimitWait bounds how long a read waits on a 429 before retrying;
	// a longer Retry-After is reported to the user instead.
	maxRateLimitWait = 30 * time.Second
	// maxListPages bounds an "every page" listing.
	maxListPages = 1000
)

// rateLimitSleep is replaced in tests.
var rateLimitSleep = time.Sleep

// CloudClient calls the cloud API (/cloud/v1) with the same host, token,
// redirect policy and body limit as Client. Its responses are the cloud
// service's own JSON, not the gateway's {success,data,error} envelope.
type CloudClient struct {
	c *Client
}

// Cloud returns the client for the cloud API.
func (c *Client) Cloud() *CloudClient {
	return &CloudClient{c: c}
}

// CloudPage is one page of a cloud list, from meta.pagination.
type CloudPage struct {
	Page         int  `json:"page"`
	PerPage      int  `json:"per_page"`
	PreviousPage *int `json:"previous_page"`
	NextPage     *int `json:"next_page"`
	LastPage     int  `json:"last_page"`
	TotalEntries int  `json:"total_entries"`
}

// CloudList is a decoded list response.
type CloudList struct {
	Data []json.RawMessage `json:"data"`
	Meta struct {
		Pagination *CloudPage `json:"pagination"`
	} `json:"meta"`
}

// ListOptions are the query parameters every cloud list accepts.
type ListOptions struct {
	// Page and PerPage select one page; both zero fetch every page.
	Page    int
	PerPage int
	// LabelSelector is a Kubernetes-style selector, e.g. "env=prod,tier!=db".
	LabelSelector string
	// Extra are list-specific filters (datacenter, type, project_id...).
	Extra url.Values
}

func (o ListOptions) query(page, perPage int) url.Values {
	q := url.Values{}
	for k, vs := range o.Extra {
		for _, v := range vs {
			if v != "" {
				q.Add(k, v)
			}
		}
	}
	if o.LabelSelector != "" {
		q.Set("label_selector", o.LabelSelector)
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if perPage > 0 {
		q.Set("per_page", strconv.Itoa(perPage))
	}
	return q
}

// List reads a cloud list. With no page asked for it follows next_page
// until the end, 100 items at a time, and returns everything.
func (cc *CloudClient) List(path string, opt ListOptions) ([]json.RawMessage, *CloudPage, error) {
	if opt.Page > 0 || opt.PerPage > 0 {
		var out CloudList
		if err := cc.Get(path, opt.query(max(opt.Page, 1), opt.PerPage), &out); err != nil {
			return nil, nil, err
		}
		return nonNil(out.Data), out.Meta.Pagination, nil
	}

	var all []json.RawMessage
	var last *CloudPage
	page := 1
	for i := 0; i < maxListPages; i++ {
		var out CloudList
		if err := cc.Get(path, opt.query(page, 100), &out); err != nil {
			return nil, nil, err
		}
		all = append(all, out.Data...)
		last = out.Meta.Pagination
		if last == nil || last.NextPage == nil || *last.NextPage <= page {
			break
		}
		page = *last.NextPage
	}
	if last != nil {
		last = &CloudPage{Page: 1, PerPage: len(all), LastPage: 1, TotalEntries: last.TotalEntries}
	}
	return nonNil(all), last, nil
}

func nonNil(items []json.RawMessage) []json.RawMessage {
	if items == nil {
		return []json.RawMessage{}
	}
	return items
}

// Get reads path; reads are retried on transient failures and short 429s.
func (cc *CloudClient) Get(path string, query url.Values, out any) error {
	return cc.do(http.MethodGet, path, query, nil, out, cloudReadRetries)
}

// Post, Put, Patch and Delete change state and are never retried: the
// outcome of a repeated create or delete is not knowable here.
func (cc *CloudClient) Post(path string, body, out any) error {
	return cc.do(http.MethodPost, path, nil, body, out, 0)
}

func (cc *CloudClient) Put(path string, body, out any) error {
	return cc.do(http.MethodPut, path, nil, body, out, 0)
}

func (cc *CloudClient) Patch(path string, body, out any) error {
	return cc.do(http.MethodPatch, path, nil, body, out, 0)
}

// Delete sends query and, when body is not nil, a JSON body.
func (cc *CloudClient) Delete(path string, query url.Values, body, out any) error {
	return cc.do(http.MethodDelete, path, query, body, out, 0)
}

// URL is the full address of a cloud API path.
func (cc *CloudClient) URL(path string, query url.Values) string {
	u := cc.c.baseURL + CloudPrefix + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

func (cc *CloudClient) do(method, path string, query url.Values, body, out any, retries int) error {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		payload = b
	}

	for attempt := 0; ; attempt++ {
		err := cc.attempt(method, cc.URL(path, query), payload, out)
		if err == nil || attempt >= retries {
			return err
		}
		wait, ok := cloudRetryWait(err, attempt)
		if !ok {
			return err
		}
		rateLimitSleep(wait)
	}
}

// cloudRetryWait says whether and how long to wait before repeating a read.
func cloudRetryWait(err error, attempt int) (time.Duration, bool) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		var netErr *transportError
		return time.Duration(attempt+1) * 2 * retryStep, errors.As(err, &netErr)
	}
	switch apiErr.Status {
	case http.StatusTooManyRequests:
		wait := time.Duration(apiErr.RetryAfter) * time.Second
		if apiErr.RetryAfter <= 0 {
			wait = time.Duration(attempt+1) * 2 * retryStep
		}
		return wait, wait <= maxRateLimitWait
	case http.StatusBadGateway:
		return time.Duration(attempt+1) * 2 * retryStep, true
	case http.StatusServiceUnavailable:
		wait := time.Duration(apiErr.RetryAfter) * time.Second
		if apiErr.RetryAfter <= 0 {
			wait = time.Duration(attempt+1) * 2 * retryStep
		}
		return wait, wait <= maxRateLimitWait
	}
	return 0, false
}

func (cc *CloudClient) attempt(method, fullURL string, payload []byte, out any) error {
	resp, raw, err := cc.c.send(method, fullURL, payload, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return cloudError(resp, raw)
	}
	if out == nil || len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	if rawOut, ok := out.(*json.RawMessage); ok {
		if !json.Valid(raw) {
			return badResponse(resp)
		}
		*rawOut = append((*rawOut)[:0], raw...)
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return badResponse(resp)
	}
	return nil
}

func badResponse(resp *http.Response) *APIError {
	return &APIError{
		Code:       "BAD_GATEWAY_RESPONSE",
		Message:    fmt.Sprintf("unexpected response from the server (HTTP %d)", resp.StatusCode),
		Status:     resp.StatusCode,
		RetryAfter: rateLimitRetryAfter(resp),
	}
}

// cloudError reads either error shape the cloud API can answer with: the
// cloud service's {"error": "message", "code": "...", "reason": "..."} or
// the gateway's own {"success": false, "error": {"code", "message"}}.
func cloudError(resp *http.Response, raw []byte) *APIError {
	var probe struct {
		Error   json.RawMessage `json:"error"`
		Code    string          `json:"code"`
		Reason  string          `json:"reason"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return badResponse(resp)
	}

	out := &APIError{Status: resp.StatusCode, RetryAfter: rateLimitRetryAfter(resp), Code: probe.Code, Reason: probe.Reason}
	var text string
	var nested struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	switch {
	case len(probe.Error) > 0 && json.Unmarshal(probe.Error, &text) == nil:
		out.Message = text
	case len(probe.Error) > 0 && json.Unmarshal(probe.Error, &nested) == nil:
		out.Message = nested.Message
		if nested.Code != "" {
			out.Code = nested.Code
		}
	default:
		out.Message = probe.Message
	}
	if out.Message == "" {
		out.Message = fmt.Sprintf("request failed with HTTP %d", resp.StatusCode)
	}
	if out.Code == "" {
		out.Code = "REQUEST_FAILED"
	}
	return out
}

// rateLimitRetryAfter is Retry-After, or RateLimit-Reset (seconds until the
// window resets) on a 429 that has no Retry-After.
func rateLimitRetryAfter(resp *http.Response) int {
	if n := retryAfter(resp); n > 0 {
		return n
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		if n, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("RateLimit-Reset"))); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// ─── Actions ─────────────────────────────────────────────────────────────────

// CloudAction is an asynchronous operation: GET /actions/{id}.
type CloudAction struct {
	ID         int64           `json:"id"`
	Command    string          `json:"command"`
	Status     string          `json:"status"` // running | success | error
	Progress   int             `json:"progress"`
	Resource   *ActionResource `json:"resource"`
	Error      *ActionError    `json:"error"`
	StartedAt  string          `json:"started_at"`
	FinishedAt *string         `json:"finished_at"`
}

type ActionResource struct {
	Type string `json:"type"`
	ID   int64  `json:"id"`
}

type ActionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// GetAction reads one action.
func (cc *CloudClient) GetAction(id int64) (*CloudAction, error) {
	var out struct {
		Data CloudAction `json:"data"`
	}
	if err := cc.Get("/actions/"+strconv.FormatInt(id, 10), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}
