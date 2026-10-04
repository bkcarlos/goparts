package user

import (
	"context"
	"errors"
	"net/http"
)

type FileBlock struct {
	BlockID     string // inner file block ID; use this as UploadMedia ParentNode
	ViewBlockID string // enclosing view block; useful for cleanup after partial failure
	RevisionID  int64
}

// CreateFileBlock appends an empty file block. Feishu returns its enclosing view
// block, so the upload target is the view's child, not the returned view ID.
func (c *Client) CreateFileBlock(ctx context.Context, documentID, parentID string, opts WriteOptions) (FileBlock, error) {
	created, err := c.AppendBlocks(ctx, documentID, parentID, []Block{{"block_type": 23, "file": map[string]string{"token": ""}}}, opts)
	if err != nil {
		return FileBlock{}, err
	}
	view := created.Children[0]
	result := FileBlock{ViewBlockID: view.ID(), RevisionID: created.RevisionID}
	children, ok := view["children"].([]any)
	if !ok || len(children) != 1 || !validID(result.ViewBlockID) {
		return result, ErrInvalidResponse
	}
	result.BlockID, _ = children[0].(string)
	if !validID(result.BlockID) {
		return result, ErrInvalidResponse
	}
	return result, nil
}

// ReplaceFile associates an uploaded media file_token with an existing file
// block. Creating the block, uploading and replacing are not a transaction.
func (c *Client) ReplaceFile(ctx context.Context, documentID, blockID, fileToken string, opts WriteOptions) (UpdateResult, error) {
	if !validID(blockID) || !validID(fileToken) {
		return UpdateResult{}, errors.New("feishu/user: file block ID and media token are required")
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
	err = c.api(ctx, http.MethodPatch, path+"?"+query.Encode(), map[string]any{"replace_file": map[string]string{"token": fileToken}}, &result)
	if err == nil && result.Block.ID() == "" {
		err = ErrInvalidResponse
	}
	return result, err
}
