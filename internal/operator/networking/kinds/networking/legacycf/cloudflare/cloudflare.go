package cloudflare

import (
	"errors"
	"github.com/cloudflare/cloudflare-go"
)

var pageOpts = cloudflare.PaginationOptions{
	PerPage: 25,
	Page:    1,
}

type Cloudflare struct {
	api *cloudflare.API
}

func New(accountName string, user string, key string, userServiceKey string) (*Cloudflare, error) {
	api, err := cloudflare.New(key, user)
	if err != nil {
		return nil, err
	}

	api.APIUserServiceKey = userServiceKey
	if accountName != "" {
		accounts, _, err := api.Accounts(cloudflare.PaginationOptions{})
		if err != nil {
			return nil, err
		}
		found := false
		for _, account := range accounts {
			if account.Name == accountName {
				found = true
				api.AccountID = account.ID
			}
		}
		if !found {
			return nil, errors.New("no account with given name found")
		}
	}

	return &Cloudflare{
		api: api,
	}, nil
}
