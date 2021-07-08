package orbiter

import (
	"fmt"
	"regexp"

	"github.com/pkg/errors"
)

var (
	ipPartRegex = `([0-9]|[1-9][0-9]|1[0-9][0-9]|2[0-4][0-9]|25[0-5])`
	ipRegex     = fmt.Sprintf(`%s\.%s\.%s\.%s`, ipPartRegex, ipPartRegex, ipPartRegex, ipPartRegex)
	cidrRegex   = fmt.Sprintf(`%s/([1-2][0-9]|3[0-2]|[0-9])`, ipRegex)

	compiledCIDR = regexp.MustCompile(fmt.Sprintf(`^(%s)$`, cidrRegex))
)

type CIDR string

type CIDRs []*CIDR

func (c CIDRs) Len() int           { return len(c) }
func (c CIDRs) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c CIDRs) Less(i, j int) bool { return *c[i] < *c[j] }

func (c CIDR) Validate() error {
	if !compiledCIDR.MatchString(string(c)) {
		return errors.Errorf("Value %s is not in valid CIDR notation. It does not match the regular expression %s", c, compiledCIDR.String())
	}
	return nil
}
