// Package contact resolves email/mobile identifiers under Feishu permissions.
package contact

import (
	"context"
	"errors"
	"github.com/bkcarlos/goparts/feishu/openapi"
	"net/url"
)

type Config = openapi.Config
type Client struct{ api *openapi.Client }

func New(cfg Config) (*Client, error) {
	api, err := openapi.New(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{api}, nil
}

type BatchRequest struct {
	Emails          []string `json:"emails,omitempty"`
	Mobiles         []string `json:"mobiles,omitempty"`
	IncludeResigned bool     `json:"include_resigned,omitempty"`
	UserIDType      string   `json:"-"`
}
type UserID struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Mobile string `json:"mobile"`
}

func (c *Client) BatchGetID(ctx context.Context, input BatchRequest) ([]UserID, error) {
	if len(input.Emails)+len(input.Mobiles) == 0 || len(input.Emails) > 50 || len(input.Mobiles) > 50 {
		return nil, errors.New("contact: provide up to 50 emails and 50 mobiles")
	}
	kind := input.UserIDType
	if kind == "" {
		kind = "open_id"
	}
	if kind != "open_id" && kind != "user_id" && kind != "union_id" {
		return nil, errors.New("contact: invalid ID type")
	}
	var out struct {
		Users []UserID `json:"user_list"`
	}
	err := c.api.Do(ctx, "POST", "/contact/v3/users/batch_get_id", url.Values{"user_id_type": {kind}}, input, &out)
	return out.Users, err
}
