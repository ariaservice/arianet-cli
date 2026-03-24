package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const defaultTimeout = 30 * time.Second

// Client is the Arianet API HTTP client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New creates a new API Client.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// ─── Core HTTP helpers ────────────────────────────────────────────────────────

func (c *Client) do(method, path string, body interface{}, out interface{}) (*Pagination, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+"/api/v1"+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var envelope Response
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode response (HTTP %d): %w", resp.StatusCode, err)
	}

	if !envelope.Success {
		if envelope.Error != nil {
			return nil, envelope.Error
		}
		return nil, fmt.Errorf("request failed with HTTP %d", resp.StatusCode)
	}

	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return nil, fmt.Errorf("decode data: %w", err)
		}
	}

	return envelope.Pagination, nil
}

func (c *Client) get(path string, out interface{}) (*Pagination, error) {
	return c.do(http.MethodGet, path, nil, out)
}

func (c *Client) post(path string, body, out interface{}) error {
	_, err := c.do(http.MethodPost, path, body, out)
	return err
}

func (c *Client) put(path string, body, out interface{}) error {
	_, err := c.do(http.MethodPut, path, body, out)
	return err
}

func (c *Client) delete(path string) error {
	_, err := c.do(http.MethodDelete, path, nil, nil)
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
	return c.delete("/auth/logout")
}

func (c *Client) ListTokens() ([]Token, error) {
	var data TokensData
	_, err := c.get("/auth/tokens", &data)
	return data.Tokens, err
}

func (c *Client) RevokeToken(id int) error {
	return c.delete(fmt.Sprintf("/auth/tokens/%d", id))
}

// ─── Regions ─────────────────────────────────────────────────────────────────

func (c *Client) ListRegions() ([]Region, error) {
	var data RegionsData
	_, err := c.get("/regions", &data)
	return data.Regions, err
}

// ─── OS Templates ─────────────────────────────────────────────────────────────

func (c *Client) ListOS() ([]OSTemplate, error) {
	var data OSData
	_, err := c.get("/os", &data)
	return data.OSTemplates, err
}

func (c *Client) ListOSByDatacenter(datacenterID int) ([]OSTemplate, error) {
	var data OSData
	_, err := c.get(fmt.Sprintf("/os/datacenter/%d", datacenterID), &data)
	return data.OSTemplates, err
}

// ─── Plans ────────────────────────────────────────────────────────────────────

func (c *Client) ListPlans() ([]Plan, error) {
	var data PlansData
	_, err := c.get("/plans", &data)
	return data.Plans, err
}

func (c *Client) GetPlan(id int) (*Plan, error) {
	var plan Plan
	_, err := c.get(fmt.Sprintf("/plans/%d", id), &plan)
	return &plan, err
}

func (c *Client) ListPlansByDatacenter(datacenterID int) ([]Plan, error) {
	var data PlansData
	_, err := c.get(fmt.Sprintf("/plans/datacenter/%d", datacenterID), &data)
	return data.Plans, err
}

// ─── Services ─────────────────────────────────────────────────────────────────

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

func (c *Client) CreateService(req CreateServiceRequest) (*CreatedService, error) {
	var created CreatedService
	err := c.post("/servers", req, &created)
	return &created, err
}

func (c *Client) DeleteService(id int) error {
	return c.delete(fmt.Sprintf("/servers/%d", id))
}

func (c *Client) GetServiceStatus(id int) (*ServiceStatus, error) {
	var status ServiceStatus
	_, err := c.get(fmt.Sprintf("/servers/%d/status", id), &status)
	return &status, err
}

func (c *Client) RestartService(id int) error {
	return c.post(fmt.Sprintf("/servers/%d/restart", id), nil, nil)
}

func (c *Client) PowerOnService(id int) error {
	return c.post(fmt.Sprintf("/servers/%d/power-on", id), nil, nil)
}

func (c *Client) PowerOffService(id int) error {
	return c.post(fmt.Sprintf("/servers/%d/power-off", id), nil, nil)
}

func (c *Client) RenameService(id int, hostname string) error {
	return c.post(fmt.Sprintf("/servers/%d/rename", id), map[string]string{"hostname": hostname}, nil)
}

func (c *Client) ReinstallService(id, osID int) error {
	return c.post(fmt.Sprintf("/servers/%d/reinstall", id), map[string]int{"os_id": osID}, nil)
}

func (c *Client) ToggleServiceProtection(id int) error {
	return c.post(fmt.Sprintf("/servers/%d/toggle-protection", id), nil, nil)
}

// ─── Balance ──────────────────────────────────────────────────────────────────

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

// ─── SSH Keys ─────────────────────────────────────────────────────────────────

func (c *Client) ListSSHKeys() ([]SSHKey, error) {
	var keys []SSHKey
	_, err := c.get("/ssh-keys", &keys)
	return keys, err
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
	return c.delete(fmt.Sprintf("/ssh-keys/%d", id))
}

// ─── Firewalls ────────────────────────────────────────────────────────────────

func (c *Client) ListFirewalls() ([]Firewall, error) {
	var firewalls []Firewall
	_, err := c.get("/firewalls", &firewalls)
	return firewalls, err
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

func (c *Client) DeleteFirewall(id int) error {
	return c.delete(fmt.Sprintf("/firewalls/%d", id))
}

func (c *Client) AddFirewallRule(fwID int, rule FirewallRule) error {
	return c.post(fmt.Sprintf("/firewalls/%d/rules", fwID), rule, nil)
}

func (c *Client) DeleteFirewallRule(fwID, ruleIndex int) error {
	return c.delete(fmt.Sprintf("/firewalls/%d/rules/%s", fwID, strconv.Itoa(ruleIndex)))
}
