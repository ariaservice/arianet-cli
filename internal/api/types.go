package api

import "encoding/json"

// ─── Response envelope ───────────────────────────────────────────────────────

type Response struct {
	Success    bool            `json:"success"`
	Data       json.RawMessage `json:"data,omitempty"`
	Pagination *Pagination     `json:"pagination,omitempty"`
	Error      *APIError       `json:"error,omitempty"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return e.Message
}

type Pagination struct {
	CurrentPage int   `json:"current_page"`
	PerPage     int   `json:"per_page"`
	Total       int64 `json:"total"`
	LastPage    int   `json:"last_page"`
}

// ─── Auth / User ─────────────────────────────────────────────────────────────

type User struct {
	ID       int     `json:"id"`
	Email    *string `json:"email"`
	Mobile   *string `json:"mobile"`
	Status   string  `json:"status"`
	Verified bool    `json:"verified"`
}

type Token struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Prefix    string  `json:"prefix"`
	LastUsed  *string `json:"last_used"`
	Expires   *string `json:"expires"`
	CreatedAt string  `json:"created_at"`
}

type TokensData struct {
	Tokens []Token `json:"tokens"`
}

// ─── Region / Datacenter ─────────────────────────────────────────────────────

type Region struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	DisplayName *string `json:"display_name"`
	Country     *string `json:"country"`
}

type RegionsData struct {
	Regions []Region `json:"regions"`
}

// ─── OS Template ─────────────────────────────────────────────────────────────

type OSTemplate struct {
	ID       int  `json:"id"`
	Name     string `json:"name"`
	RegionID *int   `json:"region_id"`
}

type OSData struct {
	OSTemplates []OSTemplate `json:"os_templates"`
}

// ─── Plan ────────────────────────────────────────────────────────────────────

type ResourceValue struct {
	Size int    `json:"size"`
	Unit string `json:"unit"`
}

type Plan struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	Recommended bool           `json:"recommended"`
	Cycle       *string        `json:"cycle"`
	CPU         *ResourceValue `json:"cpu,omitempty"`
	RAM         *ResourceValue `json:"ram,omitempty"`
	Storage     *ResourceValue `json:"storage,omitempty"`
	Prices      []PlanPrice    `json:"prices,omitempty"`
}

type PlanPrice struct {
	Code     string   `json:"code"`
	Currency string   `json:"currency"`
	Hourly   *float64 `json:"hourly"`
	Monthly  *float64 `json:"monthly"`
	Yearly   *float64 `json:"yearly,omitempty"`
}

type PlanGroup struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Plans []Plan `json:"plans"`
}

type PlansGroupedData struct {
	Groups []PlanGroup `json:"groups"`
}

// ─── Service ─────────────────────────────────────────────────────────────────

type Service struct {
	ID             int                `json:"id"`
	Status         string             `json:"status"`
	InstanceStatus *string            `json:"instance_status"`
	Hostname       *string            `json:"hostname"`
	Protected      *bool              `json:"protected"`
	Cycle          *string            `json:"cycle"`
	IPAddresses    []IPAddressSummary `json:"ip_addresses"`
	Plan           *PlanRef           `json:"plan,omitempty"`
	Datacenter     *DatacenterRef     `json:"datacenter,omitempty"`
	OS             *OSRef             `json:"os,omitempty"`
	CreatedAt      string             `json:"created_at"`
}

type IPAddressSummary struct {
	IP   string `json:"ip"`
	Type string `json:"type"`
}

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
	ID             int    `json:"id"`
	Status         string `json:"status"`
	InstanceStatus string `json:"instance_status"`
}

type PlanRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type DatacenterRef struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	DisplayName *string `json:"display_name"`
	Country     *string `json:"country"`
}

type OSRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type CreateServiceRequest struct {
	PlanID       int     `json:"plan_id"`
	DatacenterID int     `json:"datacenter_id"`
	OsID         int     `json:"os_id"`
	Hostname     string  `json:"hostname,omitempty"`
	ProjectID    *int    `json:"project_id,omitempty"`
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
	ID       int      `json:"id"`
	Balance  float64  `json:"balance"`
	Primary  bool     `json:"primary"`
	Status   string   `json:"status"`
	Currency *Currency `json:"currency,omitempty"`
}

type Currency struct {
	ID     int     `json:"id"`
	Code   string  `json:"code"`
	Name   string  `json:"name"`
	Symbol *string `json:"symbol"`
}

type Transaction struct {
	ID        int       `json:"id"`
	Mode      string    `json:"mode"`
	Amount    float64   `json:"amount"`
	Status    *string   `json:"status"`
	Reference string    `json:"reference"`
	Type      *string   `json:"type,omitempty"`
	Currency  *Currency `json:"currency,omitempty"`
	CreatedAt string    `json:"created_at"`
}

// ─── SSH Keys ─────────────────────────────────────────────────────────────────

type SSHKey struct {
	ID        int     `json:"id"`
	Name      *string `json:"name"`
	Key       string  `json:"key"`
	Protected bool    `json:"protected"`
	CreatedAt string  `json:"created_at"`
}

type CreateSSHKeyRequest struct {
	Name         string  `json:"name"`
	PublicKey    string  `json:"public_key"`
	DatacenterID *int    `json:"datacenter_id,omitempty"`
}

type UpdateSSHKeyRequest struct {
	Name        *string `json:"name,omitempty"`
	IsProtected *bool   `json:"is_protected,omitempty"`
}

// ─── Firewalls ────────────────────────────────────────────────────────────────

type Firewall struct {
	ID        int            `json:"id"`
	Name      *string        `json:"name"`
	Protected bool           `json:"protected"`
	Rules     []FirewallRule `json:"rules"`
	CreatedAt string         `json:"created_at"`
}

type FirewallRule struct {
	Direction string `json:"direction"`
	Protocol  string `json:"protocol"`
	PortRange string `json:"port_range,omitempty"`
	Source    string `json:"source,omitempty"`
	Action    string `json:"action"`
}

type CreateFirewallRequest struct {
	Name         string         `json:"name"`
	DatacenterID *int           `json:"datacenter_id,omitempty"`
	Rules        []FirewallRule `json:"rules,omitempty"`
}
