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

	// Filled in by the client, not part of the wire format.
	Status     int `json:"-"`
	RetryAfter int `json:"-"` // seconds, from the Retry-After header
}

func (e *APIError) Error() string {
	return e.Message
}

// IsNotFound reports whether err is an API 404.
func IsNotFound(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && (apiErr.Status == 404 || apiErr.Code == "NOT_FOUND")
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

// Region groups datacenters. The Datacenter ID is what every other endpoint
// calls datacenter_id.
type Region struct {
	ID          int          `json:"id"`
	Name        string       `json:"name"`
	IsRegion    bool         `json:"is_region"`
	Status      string       `json:"status"`
	IsDefault   bool         `json:"is_default"`
	CountryCode *string      `json:"country_code"`
	Flag        string       `json:"flag"`
	Datacenters []Datacenter `json:"datacenters"`
}

type Datacenter struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	IsDefault   bool    `json:"is_default"`
	Status      string  `json:"status"`
	CountryCode *string `json:"country_code"`
	Flag        string  `json:"flag"`
	// Supports says which optional resources the datacenter offers (ssh_keys,
	// firewalls); nil when the API did not report it.
	Supports map[string]bool `json:"supports,omitempty"`
}

type RegionsData struct {
	Items []Region `json:"items"`
}

// DatacenterEntry is one orderable location, flattened for display.
type DatacenterEntry struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Region  string `json:"region"`
	Country string `json:"country"`
	Status  string `json:"status"`

	Supports map[string]bool `json:"-"`
}

// ─── OS Template ─────────────────────────────────────────────────────────────

type OSGroup struct {
	ID        int         `json:"id"`
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	Order     int         `json:"order"`
	Templates OSTemplates `json:"templates"`
}

type OSTemplates struct {
	Data []OSTemplate `json:"data"`
}

type OSTemplate struct {
	ID       int        `json:"id"`
	Name     string     `json:"name"`
	Status   bool       `json:"status"`
	Children OSChildren `json:"children"`
}

type OSChildren struct {
	Data []OSChild `json:"data"`
}

type OSChild struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status bool   `json:"status"`
}

type OSData struct {
	Groups []OSGroup `json:"groups"`
}

// OSEntry is one installable image (a template, or one version of it),
// flattened for display. ID is the os_id.
type OSEntry struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Family string `json:"family"`
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
	Code     string `json:"code"`
	Currency string `json:"currency"`
	Hourly   string `json:"hourly"`
	Monthly  string `json:"monthly"`
	Yearly   string `json:"yearly,omitempty"`
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
	Name           *string            `json:"name"`
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
	ID             int     `json:"id"`
	Status         string  `json:"status"`
	InstanceStatus *string `json:"instance_status"`
}

// ProviderStatus returns the provider-reported status, or "" when unknown.
func (s *ServiceStatus) ProviderStatus() string {
	if s.InstanceStatus == nil {
		return ""
	}
	return *s.InstanceStatus
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
	PlanID       int    `json:"plan_id"`
	DatacenterID int    `json:"datacenter_id"`
	OsID         int    `json:"os_id"`
	Hostname     string `json:"hostname,omitempty"`
	ProjectID    *int   `json:"project_id,omitempty"`
	CurrencyID   *int   `json:"currency_id,omitempty"`
	AuthType     string `json:"auth_type,omitempty"` // password (default) | ssh
	AuthValue    string `json:"auth_value,omitempty"`
	SSHKeyID     *int   `json:"ssh_key_id,omitempty"`
}

type CreatedService struct {
	ID             int                `json:"id"`
	Status         string             `json:"status"`
	InstanceStatus *string            `json:"instance_status"`
	Name           *string            `json:"name"`
	IPAddresses    []IPAddressSummary `json:"ip_addresses"`
	Protected      bool               `json:"protected"`
	Cycle          *string            `json:"cycle"`
	Plan           *struct {
		Name *string `json:"name"`
	} `json:"plan,omitempty"`
	Datacenter *struct {
		ID *int `json:"id"`
	} `json:"datacenter,omitempty"`
	OS *struct {
		Name *string `json:"name"`
	} `json:"os,omitempty"`
	RootPassword string `json:"root_password,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// CreateResult is a created server plus how the answer was obtained.
type CreateResult struct {
	Server   *CreatedService
	Replayed bool // the answer to an earlier request with the same Idempotency-Key
}

// ActionResult is the answer to an asynchronous server operation.
type ActionResult struct {
	ServerID  int    `json:"server_id"`
	Message   string `json:"message,omitempty"`
	Name      string `json:"name,omitempty"`
	Protected *bool  `json:"protected,omitempty"`
}

type ServerAction struct {
	ID         json.Number `json:"id"`
	Type       string      `json:"type"`
	Status     string      `json:"status"`
	StartedAt  *string     `json:"started_at"`
	FinishedAt *string     `json:"finished_at"`
	CreatedAt  string      `json:"created_at"`
}

type ServerActions struct {
	ServerID int            `json:"server_id"`
	Actions  []ServerAction `json:"actions"`
}

// ─── Balance ─────────────────────────────────────────────────────────────────

type BalanceData struct {
	Wallets []Wallet `json:"wallets"`
}

type Wallet struct {
	ID       int       `json:"id"`
	Balance  float64   `json:"balance"`
	Primary  bool      `json:"primary"`
	Status   string    `json:"status"`
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

// ─── Invoices ────────────────────────────────────────────────────────────────

// Invoice numbers are text: legacy numeric values and INV-* coexist.
type Invoice struct {
	Number        string        `json:"number"`
	Status        string        `json:"status"`
	PaymentStatus string        `json:"payment_status"`
	Subtotal      float64       `json:"subtotal"`
	Discount      float64       `json:"discount"`
	Tax           float64       `json:"tax"`
	Total         float64       `json:"total"`
	IssuedAt      *string       `json:"issued_at"`
	DueAt         *string       `json:"due_at"`
	PaidAt        *string       `json:"paid_at"`
	Items         []InvoiceItem `json:"items,omitempty"`
}

type InvoiceItem struct {
	Amount      float64   `json:"amount"`
	Currency    *Currency `json:"currency,omitempty"`
	Description *string   `json:"description"`
}

// ─── SSH Keys ─────────────────────────────────────────────────────────────────

type SSHKey struct {
	ID           int     `json:"id"`
	Name         *string `json:"name"`
	Key          string  `json:"key"`
	Protected    bool    `json:"protected"`
	DatacenterID *int    `json:"datacenter_id"`
	CreatedAt    string  `json:"created_at"`
}

type CreateSSHKeyRequest struct {
	Name         string `json:"name"`
	PublicKey    string `json:"public_key"`
	DatacenterID int    `json:"datacenter_id"`
}

type UpdateSSHKeyRequest struct {
	Name        *string `json:"name,omitempty"`
	IsProtected *bool   `json:"is_protected,omitempty"`
}

// ─── Firewalls ────────────────────────────────────────────────────────────────

type Firewall struct {
	ID           int            `json:"id"`
	Name         *string        `json:"name"`
	Protected    bool           `json:"protected"`
	DatacenterID *int           `json:"datacenter_id"`
	Rules        []FirewallRule `json:"rules"`
	CreatedAt    string         `json:"created_at"`
}

type FirewallRule struct {
	ID          string  `json:"id,omitempty"`
	Direction   string  `json:"direction"` // ingress | egress
	Protocol    string  `json:"protocol"`  // tcp | udp | icmp | esp | gre
	PortRange   *string `json:"port_range,omitempty"`
	RemoteIP    *string `json:"remote_ip,omitempty"`
	Description *string `json:"description,omitempty"`
}

type CreateFirewallRequest struct {
	Name         string `json:"name"`
	DatacenterID int    `json:"datacenter_id"`
	Description  string `json:"description,omitempty"`
}

type UpdateFirewallRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type AddFirewallRuleRequest struct {
	Direction   string `json:"direction"`
	Protocol    string `json:"protocol"`
	PortRange   string `json:"port_range,omitempty"`
	RemoteIP    string `json:"remote_ip,omitempty"`
	Description string `json:"description,omitempty"`
}

type MessageData struct {
	Message string `json:"message"`
}
