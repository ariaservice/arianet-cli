package api

import "encoding/json"

// ─── Response envelope ───────────────────────────────────────────────────────

// Response is the generic API response wrapper.
type Response struct {
	Success    bool            `json:"success"`
	Data       json.RawMessage `json:"data,omitempty"`
	Pagination *Pagination     `json:"pagination,omitempty"`
	Error      *APIError       `json:"error,omitempty"`
}

// APIError represents a structured API error.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return e.Message
}

// Pagination metadata returned by list endpoints.
type Pagination struct {
	CurrentPage int `json:"current_page"`
	PerPage     int `json:"per_page"`
	Total       int `json:"total"`
	LastPage    int `json:"last_page"`
}

// ─── Auth / User ─────────────────────────────────────────────────────────────

type User struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Mobile     string `json:"mobile"`
	Status     string `json:"status"`
	IsVerified bool   `json:"is_verified"`
	CreatedAt  string `json:"created_at"`
}

type Token struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	LastUsedAt string `json:"last_used_at"`
	ExpiresAt  string `json:"expires_at"`
	CreatedAt  string `json:"created_at"`
}

// TokensData wraps the tokens list returned by GET /auth/tokens.
type TokensData struct {
	Tokens []Token `json:"tokens"`
}

// ─── Region / Datacenter ─────────────────────────────────────────────────────

type Region struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	FriendlyName string `json:"friendly_name"`
	CountryCode  string `json:"country_code"`
	Status       string `json:"status"`
}

// RegionsData wraps the regions list returned by GET /regions.
type RegionsData struct {
	Regions []Region `json:"regions"`
}

// ─── OS Template ─────────────────────────────────────────────────────────────

type OSTemplate struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	DatacenterID int    `json:"datacenter_id"`
	Status       bool   `json:"status"`
	Order        int    `json:"order"`
}

// OSData wraps the os_templates list returned by GET /os.
type OSData struct {
	OSTemplates []OSTemplate `json:"os_templates"`
}

// ─── Plan ────────────────────────────────────────────────────────────────────

type Plan struct {
	ID            int            `json:"id"`
	Name          string         `json:"name"`
	DatacenterID  int            `json:"datacenter_id"`
	IsRecommended bool           `json:"is_recommended"`
	BillingCycle  string         `json:"billing_cycle"`
	DollarPrice   float64        `json:"dollar_price"`
	Datacenter    *DatacenterRef `json:"datacenter"`
	Prices        []PlanPrice    `json:"prices,omitempty"`
}

type PlanPrice struct {
	CurrencyCode string  `json:"currency_code"`
	CurrencyName string  `json:"currency_name"`
	Price        float64 `json:"price"`
}

// PlansData wraps the plans list returned by GET /plans.
type PlansData struct {
	Plans []Plan `json:"plans"`
}

// ─── Service ─────────────────────────────────────────────────────────────────

type Service struct {
	ID                    int                `json:"id"`
	Status                string             `json:"status"`
	ProviderServiceStatus string             `json:"provider_service_status"`
	Hostname              string             `json:"hostname"`
	IsProtected           bool               `json:"is_protected"`
	BillingCycle          string             `json:"billing_cycle"`
	IPAddresses           []IPAddressSummary `json:"ip_addresses"`
	Plan                  PlanRef            `json:"plan"`
	Datacenter            DatacenterRef      `json:"datacenter"`
	OS                    OSRef              `json:"os"`
	CreatedAt             string             `json:"created_at"`
}

type IPAddressSummary struct {
	IP   string `json:"ip"`
	Type string `json:"type"` // primary | secondary | floating
}

// PrimaryIP returns the primary IP address, or empty string if none.
func (s *Service) PrimaryIP() string {
	for _, ip := range s.IPAddresses {
		if ip.Type == "primary" {
			return ip.IP
		}
	}
	if len(s.IPAddresses) > 0 {
		return s.IPAddresses[0].IP
	}
	return ""
}

type ServiceStatus struct {
	Status    string `json:"status"`
	IsRunning bool   `json:"is_running"`
}

type PlanRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type DatacenterRef struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	FriendlyName string `json:"friendly_name"`
	CountryCode  string `json:"country_code"`
}

type OSRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// CreateServiceRequest is the payload for POST /services.
type CreateServiceRequest struct {
	PlanID       int    `json:"plan_id"`
	DatacenterID int    `json:"datacenter_id"`
	OSID         int    `json:"os_id"`
	Hostname     string `json:"hostname"`
	SSHKeyID     int    `json:"ssh_key_id,omitempty"`
	FirewallID   int    `json:"firewall_id,omitempty"`
}

type CreatedService struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
}

// ─── Balance ─────────────────────────────────────────────────────────────────

type BalanceData struct {
	Wallets []Wallet `json:"wallets"`
}

type Wallet struct {
	ID        int      `json:"id"`
	Balance   float64  `json:"balance"`
	IsDefault bool     `json:"is_default"`
	Status    string   `json:"status"`
	Currency  Currency `json:"currency"`
}

type Currency struct {
	ID     int    `json:"id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
}

type Transaction struct {
	ID        int      `json:"id"`
	Mode      string   `json:"mode"`
	Amount    float64  `json:"amount"`
	Status    string   `json:"status"`
	Type      string   `json:"type"`
	Currency  Currency `json:"currency"`
	CreatedAt string   `json:"created_at"`
}

// ─── SSH Keys ─────────────────────────────────────────────────────────────────

type SSHKey struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
	CreatedAt   string `json:"created_at"`
}

type CreateSSHKeyRequest struct {
	Name         string `json:"name"`
	PublicKey    string `json:"public_key"`
	DatacenterID int    `json:"datacenter_id"`
}

type UpdateSSHKeyRequest struct {
	Name string `json:"name"`
}

// ─── Firewalls ────────────────────────────────────────────────────────────────

type Firewall struct {
	ID           int            `json:"id"`
	Name         string         `json:"name"`
	DatacenterID int            `json:"datacenter_id"`
	Rules        []FirewallRule `json:"rules"`
	CreatedAt    string         `json:"created_at"`
}

type FirewallRule struct {
	Direction string `json:"direction"`
	Protocol  string `json:"protocol"`
	PortRange string `json:"port_range"`
	Source    string `json:"source"`
	Action    string `json:"action"`
}

type CreateFirewallRequest struct {
	Name         string         `json:"name"`
	DatacenterID int            `json:"datacenter_id"`
	Rules        []FirewallRule `json:"rules,omitempty"`
}
