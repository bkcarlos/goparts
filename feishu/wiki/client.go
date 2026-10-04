// Package wiki resolves Wiki node tokens into document or Bitable object tokens.
package wiki

import (
	"context"
	"errors"
	"github.com/bkcarlos/goparts/feishu/openapi"
	"net/url"
	"strings"
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

type Node struct {
	SpaceID         string `json:"space_id"`
	NodeToken       string `json:"node_token"`
	ObjToken        string `json:"obj_token"`
	ObjType         string `json:"obj_type"`
	Title           string `json:"title"`
	ParentNodeToken string `json:"parent_node_token"`
	HasChild        bool   `json:"has_child"`
}

func (c *Client) GetNode(ctx context.Context, token string) (Node, error) {
	if !openapi.ValidID(token) {
		return Node{}, errors.New("wiki: invalid token")
	}
	var out struct {
		Node Node `json:"node"`
	}
	err := c.api.Do(ctx, "GET", "/wiki/v2/spaces/get_node", url.Values{"token": {token}}, nil, &out)
	return out.Node, err
}

// Resolve extracts only a token from a Wiki URL, never sends credentials to its host.
func (c *Client) Resolve(ctx context.Context, linkOrToken string) (Node, error) {
	token := linkOrToken
	if strings.Contains(linkOrToken, "://") {
		u, err := url.Parse(linkOrToken)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return Node{}, errors.New("wiki: invalid link")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 2 || parts[0] != "wiki" {
			return Node{}, errors.New("wiki: expected /wiki/token")
		}
		token = parts[1]
	}
	return c.GetNode(ctx, token)
}
func (c *Client) ResolveBitable(ctx context.Context, linkOrToken string) (string, error) {
	node, err := c.Resolve(ctx, linkOrToken)
	if err != nil {
		return "", err
	}
	if node.ObjType != "bitable" || node.ObjToken == "" {
		return "", errors.New("wiki: node is not a Bitable")
	}
	return node.ObjToken, nil
}
