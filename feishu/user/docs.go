package user

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"
)

type Document struct {
	ID         string `json:"document_id"`
	RevisionID int64  `json:"revision_id"`
	Title      string `json:"title"`
}

// Block preserves the provider's full JSON block shape, including rich content.
type Block map[string]any

func (b Block) ID() string { id, _ := b["block_id"].(string); return id }

func Paragraph(text string) Block {
	return Block{"block_type": 2, "text": map[string]any{"elements": textElements(text)}}
}

func textElements(text string) []any {
	return []any{map[string]any{"text_run": map[string]string{"content": text}}}
}

type PageOptions struct {
	PageSize   int // zero uses server default (500); maximum 500
	PageToken  string
	RevisionID *int64 // nil uses latest revision; set to pin a paginated read
}
type BlockPage struct {
	Items     []Block `json:"items"`
	HasMore   bool    `json:"has_more"`
	PageToken string  `json:"page_token"`
}
type WriteOptions struct {
	RevisionID  *int64 // nil uses latest (-1)
	ClientToken string // caller-supplied idempotency key; no automatic retry
}
type AppendResult struct {
	Children    []Block `json:"children"`
	RevisionID  int64   `json:"document_revision_id"`
	ClientToken string  `json:"client_token"`
}
type UpdateResult struct {
	Block       Block  `json:"block"`
	RevisionID  int64  `json:"document_revision_id"`
	ClientToken string `json:"client_token"`
}

func (c *Client) CreateDocument(ctx context.Context, title, folderToken string) (Document, error) {
	if !utf8.ValidString(title) || utf8.RuneCountInString(title) > 800 {
		return Document{}, errors.New("feishu/user: title must be UTF-8 and at most 800 characters")
	}
	if folderToken != "" && !validID(folderToken) {
		return Document{}, errors.New("feishu/user: invalid folder token")
	}
	input := struct {
		Title       string `json:"title,omitempty"`
		FolderToken string `json:"folder_token,omitempty"`
	}{title, folderToken}
	var result struct {
		Document Document `json:"document"`
	}
	err := c.api(ctx, http.MethodPost, "/docx/v1/documents", input, &result)
	if err == nil && result.Document.ID == "" {
		err = ErrInvalidResponse
	}
	return result.Document, err
}

func (c *Client) GetDocument(ctx context.Context, documentID string) (Document, error) {
	path, err := documentPath(documentID)
	if err != nil {
		return Document{}, err
	}
	var result struct {
		Document Document `json:"document"`
	}
	err = c.api(ctx, http.MethodGet, path, nil, &result)
	if err == nil && result.Document.ID == "" {
		err = ErrInvalidResponse
	}
	return result.Document, err
}

func (c *Client) ReadDocument(ctx context.Context, documentID string) (string, error) {
	path, err := documentPath(documentID)
	if err != nil {
		return "", err
	}
	var result struct {
		Content *string `json:"content"`
	}
	if err := c.api(ctx, http.MethodGet, path+"/raw_content", nil, &result); err != nil {
		return "", err
	}
	if result.Content == nil {
		return "", ErrInvalidResponse
	}
	return *result.Content, nil
}

// ListBlocks retrieves one page. Follow PageToken explicitly while HasMore is true.
func (c *Client) ListBlocks(ctx context.Context, documentID string, opts PageOptions) (BlockPage, error) {
	path, err := documentPath(documentID)
	if err != nil {
		return BlockPage{}, err
	}
	if opts.PageSize < 0 || opts.PageSize > 500 {
		return BlockPage{}, errors.New("feishu/user: page size must be in [0,500]")
	}
	query, err := writeQuery(WriteOptions{RevisionID: opts.RevisionID})
	if err != nil {
		return BlockPage{}, err
	}
	if opts.PageSize != 0 {
		query.Set("page_size", strconv.Itoa(opts.PageSize))
	}
	if opts.PageToken != "" {
		query.Set("page_token", opts.PageToken)
	}
	var result BlockPage
	err = c.api(ctx, http.MethodGet, path+"/blocks?"+query.Encode(), nil, &result)
	if err == nil && (result.Items == nil || (result.HasMore && (result.PageToken == "" || result.PageToken == opts.PageToken))) {
		err = ErrInvalidResponse
	}
	return result, err
}

// AppendBlocks appends up to 50 blocks under parentID (empty uses document root).
// Multiple calls are not transactional; callers control batching and retries.
func (c *Client) AppendBlocks(ctx context.Context, documentID, parentID string, blocks []Block, opts WriteOptions) (AppendResult, error) {
	path, err := blockPath(documentID, parentID)
	if err != nil {
		return AppendResult{}, err
	}
	if len(blocks) == 0 || len(blocks) > 50 {
		return AppendResult{}, errors.New("feishu/user: append requires 1 to 50 blocks")
	}
	query, err := writeQuery(opts)
	if err != nil {
		return AppendResult{}, err
	}
	input := struct {
		Children []Block `json:"children"`
		Index    int     `json:"index"`
	}{blocks, -1}
	var result AppendResult
	err = c.api(ctx, http.MethodPost, path+"/children?"+query.Encode(), input, &result)
	if err == nil && len(result.Children) != len(blocks) {
		err = ErrInvalidResponse
	}
	return result, err
}

func (c *Client) AppendText(ctx context.Context, documentID, text string, opts WriteOptions) (AppendResult, error) {
	return c.AppendBlocks(ctx, documentID, "", []Block{Paragraph(text)}, opts)
}

// UpdateText replaces a text-capable block's elements (including inline formatting).
func (c *Client) UpdateText(ctx context.Context, documentID, blockID, text string, opts WriteOptions) (UpdateResult, error) {
	if blockID == "" {
		return UpdateResult{}, errors.New("feishu/user: block ID is required for updates")
	}
	path, err := blockPath(documentID, blockID)
	if err != nil {
		return UpdateResult{}, err
	}
	query, err := writeQuery(opts)
	if err != nil {
		return UpdateResult{}, err
	}
	var result UpdateResult
	err = c.api(ctx, http.MethodPatch, path+"?"+query.Encode(), map[string]any{"update_text_elements": map[string]any{"elements": textElements(text)}}, &result)
	if err == nil && result.Block.ID() == "" {
		err = ErrInvalidResponse
	}
	return result, err
}

func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func documentPath(id string) (string, error) {
	if !validID(id) {
		return "", errors.New("feishu/user: use a document ID, not a URL or wiki token")
	}
	return "/docx/v1/documents/" + id, nil
}
func blockPath(documentID, blockID string) (string, error) {
	path, err := documentPath(documentID)
	if err != nil {
		return "", err
	}
	if blockID == "" {
		blockID = documentID
	}
	if !validID(blockID) {
		return "", errors.New("feishu/user: invalid block ID")
	}
	return path + "/blocks/" + blockID, nil
}
func writeQuery(opts WriteOptions) (url.Values, error) {
	revision := int64(-1)
	if opts.RevisionID != nil {
		revision = *opts.RevisionID
	}
	if revision < -1 {
		return nil, errors.New("feishu/user: invalid document revision")
	}
	query := url.Values{"document_revision_id": {strconv.FormatInt(revision, 10)}}
	if opts.ClientToken != "" {
		query.Set("client_token", opts.ClientToken)
	}
	return query, nil
}
