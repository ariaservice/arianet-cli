package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/arianet/arianet-cli/pkg/version"
)

const defaultTimeout = 30 * time.Second

// retryStep is the pause unit between retries (attempt n waits n*2 steps).
var retryStep = time.Second

// Client is the Arianet API HTTP client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New creates a new API Client.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// ─── Core HTTP helpers ────────────────────────────────────────────────────────

type reqOptions struct {
	idempotencyKey string
	// retries is the number of extra attempts after a transport error or a
	// 502/503. Only safe for reads and for requests carrying an
	// Idempotency-Key.
	retries int
	// retryConflict also repeats on 409, which an idempotent request gets
	// while an earlier attempt with the same key is still being processed.
	retryConflict bool
}

// meta describes how the API answered, beyond the decoded payload.
type meta struct {
	status     int
	replayed   bool
	pagination *Pagination
}

func (c *Client) do(method, path string, body, out interface{}, opt reqOptions) (meta, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return meta{}, fmt.Errorf("marshal request: %w", err)
		}
		payload = b
	}

	var (
		m   meta
		err error
	)
	for attempt := 0; ; attempt++ {
		m, err = c.attempt(method, path, payload, out, opt)
		if err == nil || attempt >= opt.retries || !retryable(err, opt) {
			return m, err
		}
		time.Sleep(time.Duration(attempt+1) * 2 * retryStep)
	}
}

// retryable reports whether a failed attempt is worth repeating: the request
// never got an answer, or the gateway could not reach core.
func retryable(err error, opt reqOptions) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusBadGateway:
			return true
		case http.StatusServiceUnavailable:
			return apiErr.RetryAfter == 0
		case http.StatusConflict:
			return opt.retryConflict
		}
		return false
	}
	var netErr *transportError
	return errors.As(err, &netErr)
}

type transportError struct{ err error }

func (e *transportError) Error() string { return "request failed: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

func (c *Client) attempt(method, path string, payload []byte, out interface{}, opt reqOptions) (meta, error) {
	var bodyReader io.Reader
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, c.baseURL+"/api/v1"+path, bodyReader)
	if err != nil {
		return meta{}, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "arianet-cli/"+version.Version)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if opt.idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", opt.idempotencyKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return meta{}, &transportError{err}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return meta{}, &transportError{err}
	}

	m := meta{
		status:   resp.StatusCode,
		replayed: resp.Header.Get("Idempotent-Replayed") == "true",
	}

	var envelope Response
	if err := json.Unmarshal(raw, &envelope); err != nil {
		// A proxy or load balancer answered with something that is not ours.
		return m, &APIError{
			Code:       "BAD_GATEWAY_RESPONSE",
			Message:    fmt.Sprintf("unexpected response from the server (HTTP %d)", resp.StatusCode),
			Status:     resp.StatusCode,
			RetryAfter: retryAfter(resp),
		}
	}

	if resp.StatusCode >= 300 || !envelope.Success {
		apiErr := envelope.Error
		if apiErr == nil {
			apiErr = &APIError{Code: "REQUEST_FAILED", Message: fmt.Sprintf("request failed with HTTP %d", resp.StatusCode)}
		}
		apiErr.Status = resp.StatusCode
		apiErr.RetryAfter = retryAfter(resp)
		return m, apiErr
	}

	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return m, fmt.Errorf("decode data: %w", err)
		}
	}

	m.pagination = envelope.Pagination
	return m, nil
}

func retryAfter(resp *http.Response) int {
	n, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (c *Client) get(path string, out interface{}) (*Pagination, error) {
	m, err := c.do(http.MethodGet, path, nil, out, reqOptions{retries: 2})
	return m.pagination, err
}

func (c *Client) post(path string, body, out interface{}) error {
	_, err := c.do(http.MethodPost, path, body, out, reqOptions{})
	return err
}

func (c *Client) put(path string, body, out interface{}) error {
	_, err := c.do(http.MethodPut, path, body, out, reqOptions{})
	return err
}

func (c *Client) delete(path string, out interface{}) error {
	_, err := c.do(http.MethodDelete, path, nil, out, reqOptions{})
	return err
}

func paginate(page, limit int) string {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 15
	}
	return fmt.Sprintf("?page=%d&limit=%d", page, limit)
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

func (c *Client) GetMe() (*User, error) {
	var user User
	_, err := c.get("/auth/me", &user)
	return &user, err
}

func (c *Client) Logout() error {
	return c.delete("/auth/logout", nil)
}

func (c *Client) ListTokens() ([]Token, error) {
	var data TokensData
	_, err := c.get("/auth/tokens", &data)
	return data.Tokens, err
}

func (c *Client) RevokeToken(id int) error {
	return c.delete(fmt.Sprintf("/auth/tokens/%d", id), nil)
}

// ─── Regions ─────────────────────────────────────────────────────────────────

func (c *Client) ListRegions() ([]Region, error) {
	var data RegionsData
	_, err := c.get("/regions", &data)
	return data.Items, err
}

// ListDatacenters returns every location a server can be ordered in.
func (c *Client) ListDatacenters() ([]DatacenterEntry, error) {
	regions, err := c.ListRegions()
	if err != nil {
		return nil, err
	}
	return FlattenDatacenters(regions), nil
}

// FlattenDatacenters turns the region tree into a flat list.
func FlattenDatacenters(regions []Region) []DatacenterEntry {
	var out []DatacenterEntry
	for _, r := range regions {
		for _, d := range r.Datacenters {
			country := ""
			if d.CountryCode != nil {
				country = *d.CountryCode
			} else if r.CountryCode != nil {
				country = *r.CountryCode
			}
			out = append(out, DatacenterEntry{
				ID:      d.ID,
				Name:    d.Name,
				Region:  r.Name,
				Country: country,
				Status:  d.Status,
			})
		}
	}
	return out
}

// ─── OS Templates ─────────────────────────────────────────────────────────────

func (c *Client) ListOSByDatacenter(datacenterID int) ([]OSEntry, error) {
	var data OSData
	if _, err := c.get(fmt.Sprintf("/os/datacenter/%d", datacenterID), &data); err != nil {
		return nil, err
	}
	return FlattenOS(data.Groups), nil
}

// FlattenOS lists installable images. A template with versions is replaced by
// its versions; a template without any is listed itself.
func FlattenOS(groups []OSGroup) []OSEntry {
	var out []OSEntry
	for _, g := range groups {
		for _, t := range g.Templates.Data {
			if len(t.Children.Data) == 0 {
				if t.Status {
					out = append(out, OSEntry{ID: t.ID, Name: t.Name, Family: g.Name})
				}
				continue
			}
			for _, ch := range t.Children.Data {
				if !ch.Status {
					continue
				}
				name := strings.TrimSpace(ch.Name)
				if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(t.Name)) {
					name = strings.TrimSpace(t.Name + " " + name)
				}
				out = append(out, OSEntry{ID: ch.ID, Name: name, Family: g.Name})
			}
		}
	}
	return out
}

// ─── Plans ────────────────────────────────────────────────────────────────────

func (c *Client) GetPlan(id int) (*Plan, error) {
	var plan Plan
	_, err := c.get(fmt.Sprintf("/plans/%d", id), &plan)
	return &plan, err
}

func (c *Client) ListPlansByDatacenter(datacenterID int) ([]Plan, error) {
	groups, err := c.ListPlansByDatacenterGrouped(datacenterID)
	if err != nil {
		return nil, err
	}
	var plans []Plan
	for _, g := range groups {
		plans = append(plans, g.Plans...)
	}
	return plans, nil
}

func (c *Client) ListPlansByDatacenterGrouped(datacenterID int) ([]PlanGroup, error) {
	var data PlansGroupedData
	_, err := c.get(fmt.Sprintf("/plans/datacenter/%d", datacenterID), &data)
	return data.Groups, err
}

// ─── Servers ──────────────────────────────────────────────────────────────────

func (c *Client) ListServices(page, limit int, status string) ([]Service, *Pagination, error) {
	path := "/servers" + paginate(page, limit)
	if status != "" {
		path += "&status=" + url.QueryEscape(status)
	}
	var services []Service
	pagination, err := c.get(path, &services)
	return services, pagination, err
}

func (c *Client) GetService(id int) (*Service, error) {
	var service Service
	_, err := c.get(fmt.Sprintf("/servers/%d", id), &service)
	return &service, err
}

// CreateService orders a server. The idempotency key makes a retry safe: the
// same key always yields the same server, never a second one, so transport
// errors are retried automatically.
func (c *Client) CreateService(req CreateServiceRequest, idempotencyKey string) (*CreateResult, error) {
	var created CreatedService
	m, err := c.do(http.MethodPost, "/servers", req, &created, reqOptions{idempotencyKey: idempotencyKey, retries: 5, retryConflict: true})
	if err != nil {
		return nil, err
	}
	return &CreateResult{Server: &created, Replayed: m.replayed}, nil
}

func (c *Client) DeleteService(id int) (*ActionResult, error) {
	var res ActionResult
	err := c.delete(fmt.Sprintf("/servers/%d", id), &res)
	return &res, err
}

func (c *Client) GetServiceStatus(id int) (*ServiceStatus, error) {
	var status ServiceStatus
	_, err := c.get(fmt.Sprintf("/servers/%d/status", id), &status)
	return &status, err
}

func (c *Client) ListServiceActions(id, limit int) (*ServerActions, error) {
	var actions ServerActions
	path := fmt.Sprintf("/servers/%d/actions", id)
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	_, err := c.get(path, &actions)
	return &actions, err
}

func (c *Client) serverAction(id int, action string, body interface{}) (*ActionResult, error) {
	var res ActionResult
	err := c.post(fmt.Sprintf("/servers/%d/%s", id, action), body, &res)
	return &res, err
}

func (c *Client) RestartService(id int) (*ActionResult, error) {
	return c.serverAction(id, "restart", nil)
}

func (c *Client) PowerOnService(id int) (*ActionResult, error) {
	return c.serverAction(id, "power-on", nil)
}

func (c *Client) PowerOffService(id int) (*ActionResult, error) {
	return c.serverAction(id, "power-off", nil)
}

func (c *Client) RenameService(id int, name string) (*ActionResult, error) {
	return c.serverAction(id, "rename", map[string]string{"name": name})
}

func (c *Client) ReinstallService(id, osID int) (*ActionResult, error) {
	return c.serverAction(id, "reinstall", map[string]int{"os_id": osID})
}

// SetServiceProtection turns deletion protection on or off explicitly.
func (c *Client) SetServiceProtection(id int, enabled bool) (*ActionResult, error) {
	return c.serverAction(id, "toggle-protection", map[string]bool{"enabled": enabled})
}

// ToggleServiceProtection flips the current protection state.
func (c *Client) ToggleServiceProtection(id int) (*ActionResult, error) {
	return c.serverAction(id, "toggle-protection", nil)
}

// ─── Balance & invoices ───────────────────────────────────────────────────────

func (c *Client) GetBalance() (*BalanceData, error) {
	var balance BalanceData
	_, err := c.get("/balance", &balance)
	return &balance, err
}

func (c *Client) ListTransactions(page, limit int) ([]Transaction, *Pagination, error) {
	path := "/balance/transactions" + paginate(page, limit)
	var txs []Transaction
	pagination, err := c.get(path, &txs)
	return txs, pagination, err
}

func (c *Client) ListInvoices(page, limit int, status string) ([]Invoice, *Pagination, error) {
	path := "/invoices" + paginate(page, limit)
	if status != "" {
		path += "&status=" + url.QueryEscape(status)
	}
	var invoices []Invoice
	pagination, err := c.get(path, &invoices)
	return invoices, pagination, err
}

func (c *Client) GetInvoice(number string) (*Invoice, error) {
	var invoice Invoice
	_, err := c.get("/invoices/"+url.PathEscape(number), &invoice)
	return &invoice, err
}

// ─── SSH Keys ─────────────────────────────────────────────────────────────────

func (c *Client) ListSSHKeys(page, limit int) ([]SSHKey, *Pagination, error) {
	var keys []SSHKey
	pagination, err := c.get("/ssh-keys"+paginate(page, limit), &keys)
	return keys, pagination, err
}

func (c *Client) GetSSHKey(id int) (*SSHKey, error) {
	var key SSHKey
	_, err := c.get(fmt.Sprintf("/ssh-keys/%d", id), &key)
	return &key, err
}

func (c *Client) CreateSSHKey(req CreateSSHKeyRequest) (*SSHKey, error) {
	var key SSHKey
	err := c.post("/ssh-keys", req, &key)
	return &key, err
}

func (c *Client) UpdateSSHKey(id int, req UpdateSSHKeyRequest) (*SSHKey, error) {
	var key SSHKey
	err := c.put(fmt.Sprintf("/ssh-keys/%d", id), req, &key)
	return &key, err
}

func (c *Client) DeleteSSHKey(id int) error {
	return c.delete(fmt.Sprintf("/ssh-keys/%d", id), nil)
}

// ─── Firewalls ────────────────────────────────────────────────────────────────

func (c *Client) ListFirewalls(page, limit int) ([]Firewall, *Pagination, error) {
	var firewalls []Firewall
	pagination, err := c.get("/firewalls"+paginate(page, limit), &firewalls)
	return firewalls, pagination, err
}

func (c *Client) GetFirewall(id int) (*Firewall, error) {
	var fw Firewall
	_, err := c.get(fmt.Sprintf("/firewalls/%d", id), &fw)
	return &fw, err
}

func (c *Client) CreateFirewall(req CreateFirewallRequest) (*Firewall, error) {
	var fw Firewall
	err := c.post("/firewalls", req, &fw)
	return &fw, err
}

func (c *Client) UpdateFirewall(id int, req UpdateFirewallRequest) (*Firewall, error) {
	var fw Firewall
	err := c.put(fmt.Sprintf("/firewalls/%d", id), req, &fw)
	return &fw, err
}

func (c *Client) DeleteFirewall(id int) error {
	return c.delete(fmt.Sprintf("/firewalls/%d", id), nil)
}

func (c *Client) AddFirewallRule(fwID int, rule AddFirewallRuleRequest) (*FirewallRule, error) {
	var created FirewallRule
	err := c.post(fmt.Sprintf("/firewalls/%d/rules", fwID), rule, &created)
	return &created, err
}

func (c *Client) DeleteFirewallRule(fwID int, ruleID string) error {
	return c.delete(fmt.Sprintf("/firewalls/%d/rules/%s", fwID, url.PathEscape(ruleID)), nil)
}

func (c *Client) AttachFirewall(fwID int, serverIDs []int) error {
	return c.post(fmt.Sprintf("/firewalls/%d/attach", fwID), map[string][]int{"server_ids": serverIDs}, nil)
}

func (c *Client) DetachFirewall(fwID int, serverIDs []int) error {
	return c.post(fmt.Sprintf("/firewalls/%d/detach", fwID), map[string][]int{"server_ids": serverIDs}, nil)
}
