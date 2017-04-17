package client

import (
	"encoding/json"

	"github.com/docker/engine-api/types"
	"golang.org/x/net/context"
)

// ImageCombine combine some partial images to one complete image
func (cli *Client) ImageCombine(ctx context.Context, options types.ImageCombineOptions) (types.ImageCombineResponse, error) {
	resp, err := cli.post(ctx, "/images/combine", nil, options, nil)
	if err != nil {
		return types.ImageCombineResponse{}, err
	}

	var response types.ImageCombineResponse
	err = json.NewDecoder(resp.body).Decode(&response)
	ensureReaderClosed(resp)
	return response, err
}
