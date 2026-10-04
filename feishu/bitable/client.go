// Package bitable wraps tables, records and fields with explicit pagination.
package bitable

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bkcarlos/goparts/feishu/openapi"
	"net/url"
	"strconv"
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

type Page struct {
	PageSize   int
	PageToken  string
	UserIDType string
}

func (p Page) query() (url.Values, error) {
	if p.PageSize < 0 || p.PageSize > 500 {
		return nil, errors.New("bitable: page size must be 0..500")
	}
	q := url.Values{}
	if p.PageSize > 0 {
		q.Set("page_size", strconv.Itoa(p.PageSize))
	}
	if p.PageToken != "" {
		q.Set("page_token", p.PageToken)
	}
	if p.UserIDType != "" {
		if p.UserIDType != "open_id" && p.UserIDType != "user_id" && p.UserIDType != "union_id" {
			return nil, errors.New("bitable: invalid user ID type")
		}
		q.Set("user_id_type", p.UserIDType)
	}
	return q, nil
}

type PageResult[T any] struct {
	Items     []T    `json:"items"`
	HasMore   bool   `json:"has_more"`
	PageToken string `json:"page_token"`
	Total     int    `json:"total"`
}
type Table struct {
	TableID       string `json:"table_id,omitempty"`
	Name          string `json:"name"`
	Revision      int64  `json:"revision,omitempty"`
	DefaultViewID string `json:"default_view_id,omitempty"`
}
type Field struct {
	FieldID     string          `json:"field_id,omitempty"`
	FieldName   string          `json:"field_name"`
	Type        int             `json:"type"`
	UIType      string          `json:"ui_type,omitempty"`
	Property    json.RawMessage `json:"property,omitempty"`
	Description json.RawMessage `json:"description,omitempty"`
}
type Record struct {
	RecordID string         `json:"record_id,omitempty"`
	Fields   map[string]any `json:"fields"`
}
type SearchRequest struct {
	ViewID          string          `json:"view_id,omitempty"`
	FieldNames      []string        `json:"field_names,omitempty"`
	Sort            []Sort          `json:"sort,omitempty"`
	Filter          json.RawMessage `json:"filter,omitempty"`
	AutomaticFields bool            `json:"automatic_fields,omitempty"`
}
type Sort struct {
	FieldName string `json:"field_name"`
	Desc      bool   `json:"desc"`
}

func (c *Client) do(ctx context.Context, method string, segments []string, page Page, in, out any) error {
	for _, id := range segments {
		if !openapi.ValidID(id) {
			return errors.New("bitable: invalid path identifier")
		}
	}
	q, err := page.query()
	if err != nil {
		return err
	}
	return c.api.Do(ctx, method, "/bitable/v1/apps/"+strings.Join(segments, "/"), q, in, out)
}
func (c *Client) ListTables(ctx context.Context, app string, page Page) (PageResult[Table], error) {
	var out PageResult[Table]
	err := c.do(ctx, "GET", []string{app, "tables"}, page, nil, &out)
	return out, err
}
func (c *Client) CreateTable(ctx context.Context, app, name string) (Table, error) {
	var out struct {
		TableID       string `json:"table_id"`
		DefaultViewID string `json:"default_view_id"`
	}
	if strings.TrimSpace(name) == "" {
		return Table{}, errors.New("bitable: table name required")
	}
	err := c.do(ctx, "POST", []string{app, "tables"}, Page{}, map[string]any{"table": map[string]string{"name": name}}, &out)
	return Table{TableID: out.TableID, Name: name, DefaultViewID: out.DefaultViewID}, err
}
func (c *Client) UpdateTable(ctx context.Context, app, table, name string) error {
	if name == "" {
		return errors.New("bitable: name required")
	}
	return c.do(ctx, "PATCH", []string{app, "tables", table}, Page{}, map[string]string{"name": name}, nil)
}
func (c *Client) DeleteTable(ctx context.Context, app, table string) error {
	return c.do(ctx, "DELETE", []string{app, "tables", table}, Page{}, nil, nil)
}
func (c *Client) batch(ctx context.Context, app, table, operation string, records []Record, page Page) ([]Record, error) {
	if len(records) < 1 || len(records) > 500 {
		return nil, errors.New("bitable: batch must contain 1..500 records")
	}
	for _, r := range records {
		if len(r.Fields) == 0 || operation == "batch_update" && !openapi.ValidID(r.RecordID) {
			return nil, errors.New("bitable: invalid record")
		}
	}
	var out struct {
		Records []Record `json:"records"`
	}
	err := c.do(ctx, "POST", []string{app, "tables", table, "records", operation}, page, map[string]any{"records": records}, &out)
	return out.Records, err
}
func (c *Client) BatchCreate(ctx context.Context, app, table string, records []Record, page Page) ([]Record, error) {
	return c.batch(ctx, app, table, "batch_create", records, page)
}
func (c *Client) BatchUpdate(ctx context.Context, app, table string, records []Record, page Page) ([]Record, error) {
	return c.batch(ctx, app, table, "batch_update", records, page)
}
func (c *Client) Search(ctx context.Context, app, table string, input SearchRequest, page Page) (PageResult[Record], error) {
	var out PageResult[Record]
	err := c.do(ctx, "POST", []string{app, "tables", table, "records", "search"}, page, input, &out)
	return out, err
}
func (c *Client) ListFields(ctx context.Context, app, table string, page Page) (PageResult[Field], error) {
	var out PageResult[Field]
	err := c.do(ctx, "GET", []string{app, "tables", table, "fields"}, page, nil, &out)
	return out, err
}
func (c *Client) field(ctx context.Context, method string, path []string, input Field) (Field, error) {
	if input.FieldName == "" || input.Type < 1 {
		return Field{}, errors.New("bitable: field name/type required")
	}
	input.FieldID = ""
	var out struct {
		Field Field `json:"field"`
	}
	err := c.do(ctx, method, path, Page{}, input, &out)
	return out.Field, err
}
func (c *Client) CreateField(ctx context.Context, app, table string, input Field) (Field, error) {
	return c.field(ctx, "POST", []string{app, "tables", table, "fields"}, input)
}
func (c *Client) UpdateField(ctx context.Context, app, table, id string, input Field) (Field, error) {
	return c.field(ctx, "PUT", []string{app, "tables", table, "fields", id}, input)
}
func (c *Client) DeleteField(ctx context.Context, app, table, id string) error {
	return c.do(ctx, "DELETE", []string{app, "tables", table, "fields", id}, Page{}, nil, nil)
}
