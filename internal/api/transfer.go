package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ariaservice/arianet-cli/pkg/version"
)

// At is a client for another base URL with the same token, redirect policy
// and limits: a product API reached directly instead of through the
// gateway (a local development stack).
func (c *Client) At(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: c.token, httpClient: c.httpClient}
}

// transferClient has no overall timeout: an upload or download takes as
// long as the file needs. The redirect policy still keeps the token home.
func (cc *CloudClient) transferClient() *http.Client {
	return &http.Client{CheckRedirect: refuseUnsafeRedirect, Transport: cc.c.httpClient.Transport}
}

func (cc *CloudClient) transferRequest(method, path string, query url.Values, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, cc.URL(path, query), body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if cc.c.token != "" {
		req.Header.Set("Authorization", "Bearer "+cc.c.token)
	}
	req.Header.Set("User-Agent", "arianet-cli/"+version.Version)
	for k, v := range cc.headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// Upload sends body (size bytes, or -1 when unknown) as the raw request
// body with contentType, and decodes the JSON answer into out.
func (cc *CloudClient) Upload(method, path string, query url.Values, body io.Reader, size int64, contentType string, out any) error {
	req, err := cc.transferRequest(method, path, query, body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	resp, err := cc.transferClient().Do(req)
	if err != nil {
		return &transportError{err}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return &transportError{err}
	}
	if resp.StatusCode >= 300 {
		return cloudError(resp, raw)
	}
	if out == nil || len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	if rawOut, ok := out.(*json.RawMessage); ok {
		*rawOut = append((*rawOut)[:0], raw...)
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return badResponse(resp)
	}
	return nil
}

// Download streams a GET answer's body into w and returns the bytes written.
func (cc *CloudClient) Download(path string, query url.Values, w io.Writer) (int64, error) {
	req, err := cc.transferRequest(http.MethodGet, path, query, nil)
	if err != nil {
		return 0, err
	}
	resp, err := cc.transferClient().Do(req)
	if err != nil {
		return 0, &transportError{err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return 0, cloudError(resp, raw)
	}
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return n, fmt.Errorf("download: %w", err)
	}
	return n, nil
}
