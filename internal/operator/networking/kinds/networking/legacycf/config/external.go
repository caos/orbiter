package config

import (
	"errors"
	"fmt"
	"net"

	"github.com/caos/orbos/pkg/secret"

	core2 "github.com/caos/orbos/internal/operator/core"
	"github.com/caos/orbos/internal/operator/networking/kinds/networking/core"
	"github.com/caos/orbos/pkg/labels"
)

type ExternalConfig struct {
	Verbose       bool
	Domain        string
	IP            string
	Rules         []*Rule
	Groups        []*Group     `yaml:"groups"`
	Credentials   *Credentials `yaml:"credentials"`
	Prefix        string       `yaml:"prefix"`
	AdditionalDNS []*Subdomain `yaml:"additionalSubdomains,omitempty"`
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

func (e *ExternalConfig) Internal(namespace string, apiLabels *labels.API) (*InternalConfig, *current) {
	dom, curr := e.internalDomain()
	return &InternalConfig{
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
		return errors.New("no domain configured")
	}
	if net.ParseIP(e.IP) == nil {
		return fmt.Errorf("%s is not a valid ip address", e.IP)
	}
	return nil
}

func (e *ExternalConfig) ValidateSecrets() error {
	if e.Credentials == nil {
		return errors.New("no credentials specified")
	}

	if err := secret.ValidateSecret(e.Credentials.APIKey, e.Credentials.ExistingAPIKey); err != nil {
		return fmt.Errorf("validating api key failed: %w", err)
	}
	if err := secret.ValidateSecret(e.Credentials.User, e.Credentials.ExistingUser); err != nil {
		return fmt.Errorf("validating user failed: %w", err)
	}
	if err := secret.ValidateSecret(e.Credentials.UserServiceKey, e.Credentials.ExistingUserServiceKey); err != nil {
		return fmt.Errorf("validating userservice key failed: %w", err)
	}
	return nil
}

func (e *ExternalConfig) internalDomain() (*InternalDomain, *current) {

	// TODO: Remove
	subdomains := []*Subdomain{
		subdomain("accounts", e.IP),
		subdomain("api", e.IP),
		subdomain("console", e.IP),
		subdomain("issuer", e.IP),
	}
	for _, sd := range e.AdditionalDNS {
		subdomains = append(subdomains, sd)
	}

	return &InternalDomain{
			Domain:     e.Domain,
			Subdomains: subdomains,
			Rules:      e.Rules,
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

func subdomain(subdomain string, ip string) *Subdomain {
	return &Subdomain{
		Subdomain: subdomain,
		IP:        ip,
		Proxied:   true,
		TTL:       0,
		Type:      "A",
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
