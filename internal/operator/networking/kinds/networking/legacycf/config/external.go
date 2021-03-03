package config

import (
	"errors"
	"strings"

	core2 "github.com/caos/orbos/internal/operator/core"
	"github.com/caos/orbos/internal/operator/networking/kinds/networking/core"
	"github.com/caos/orbos/pkg/labels"

	"github.com/caos/orbos/internal/operator/orbiter"
)

type ExternalConfig struct {
	AccountName   string `yaml:"accountName"`
	Verbose       bool
	Domain        string
	IP            orbiter.IPAddress
	Rules         []*Rule
	Groups        []*Group      `yaml:"groups"`
	Credentials   *Credentials  `yaml:"credentials"`
	Prefix        string        `yaml:"prefix"`
	AdditionalDNS []*Subdomain  `yaml:"additionalSubdomains,omitempty"`
	LoadBalancer  *LoadBalancer `yaml:"loadBalancer,omitempty"`
}

func (i *ExternalConfig) IsZero() bool {
	if (i.Credentials == nil || i.Credentials.IsZero()) &&
		!i.Verbose &&
		i.Domain == "" &&
		i.IP == "" &&
		i.Groups == nil &&
		i.Prefix == "" &&
		i.Rules == nil &&
		i.AdditionalDNS == nil {
		return true
	}
	return false
}

func (e *ExternalConfig) Internal(id, namespace string, apiLabels *labels.API) (*InternalConfig, *current) {
	dom, curr := e.internalDomain()
	return &InternalConfig{
		AccountName:        e.AccountName,
		ID:                 id,
		Domains:            []*InternalDomain{dom},
		Groups:             e.Groups,
		Credentials:        e.Credentials,
		Prefix:             e.Prefix,
		Namespace:          namespace,
		OriginCASecretName: curr.tlsCertName,
		Labels:             apiLabels,
	}, curr
}

func (e *ExternalConfig) Validate() error {
	if e == nil {
		return errors.New("domain not found")
	}
	if e.Domain == "" {
		return errors.New("No domain configured")
	}
	return e.IP.Validate()
}

func (e *ExternalConfig) internalDomain() (*InternalDomain, *current) {
	subdomains := []*Subdomain{}
	// TODO: Remove
	if e.LoadBalancer != nil && e.LoadBalancer.Enabled {
		lbName := GetLBName(e.Domain)
		subdomains = append(subdomains,
			subdomain("accounts", lbName, "CNAME"),
			subdomain("api", lbName, "CNAME"),
			subdomain("console", lbName, "CNAME"),
			subdomain("issuer", lbName, "CNAME"),
		)
	} else {
		subdomains = append(subdomains,
			subdomain("accounts", string(e.IP), "A"),
			subdomain("api", string(e.IP), "A"),
			subdomain("console", string(e.IP), "A"),
			subdomain("issuer", string(e.IP), "A"),
		)
	}
	for _, sd := range e.AdditionalDNS {
		subdomains = append(subdomains, sd)
	}

	lb := &LoadBalancer{}
	if e.LoadBalancer != nil {
		lb.Enabled = e.LoadBalancer.Enabled
		lb.Create = e.LoadBalancer.Create
		lb.Region = e.LoadBalancer.Region
		lb.ClusterID = e.LoadBalancer.ClusterID
	}

	return &InternalDomain{
			FloatingIP:   string(e.IP),
			Domain:       e.Domain,
			Subdomains:   subdomains,
			Rules:        e.Rules,
			LoadBalancer: lb,
		},
		&current{
			domain:            e.Domain,
			issureSubdomain:   "issuer",
			consoleSubdomain:  "console",
			apiSubdomain:      "api",
			accountsSubdomain: "accounts",
			tlsCertName:       "tls-cert-wildcard",
		}
}
func GetLBName(domain string) string {
	return strings.Join([]string{"lb", domain}, ".")
}

func subdomain(subdomain string, target string, ty string) *Subdomain {
	return &Subdomain{
		Subdomain: subdomain,
		IP:        target,
		Proxied:   true,
		TTL:       0,
		Type:      ty,
	}
}

var _ core.NetworkingCurrent = (*current)(nil)

type current struct {
	domain            string `yaml:"-"`
	issureSubdomain   string `yaml:"-"`
	consoleSubdomain  string `yaml:"-"`
	apiSubdomain      string `yaml:"-"`
	accountsSubdomain string `yaml:"-"`
	tlsCertName       string `yaml:"-"`
	ReadyCertificate  core2.EnsureFunc
}

func (c *current) GetDomain() string {
	return c.domain
}
func (c *current) GetIssuerSubDomain() string {
	return c.issureSubdomain
}
func (c *current) GetConsoleSubDomain() string {
	return c.consoleSubdomain
}
func (c *current) GetAPISubDomain() string {
	return c.apiSubdomain
}
func (c *current) GetAccountsSubDomain() string {
	return c.accountsSubdomain
}
func (c *current) GetReadyCertificate() core2.EnsureFunc {
	return c.ReadyCertificate
}
func (c *current) GetTlsCertName() string {
	return c.tlsCertName
}
