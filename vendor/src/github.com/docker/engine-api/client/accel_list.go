package client

import (
	"encoding/json"
	"net/url"

	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/filters"
	"golang.org/x/net/context"
)

// AccelList returns the accelerators configured in the docker host.
func (cli *Client) AccelList(ctx context.Context, filter filters.Args) (types.AccelsListResponse, error) {
	var accels types.AccelsListResponse
	query := url.Values{}

	if filter.Len() > 0 {
		filterJSON, err := filters.ToParam(filter)
		if err != nil {
			return accels, err
		}
		query.Set("filters", filterJSON)
	}
	resp, err := cli.get(ctx, "/accelerators/slots", query, nil)
	if err != nil {
		return accels, err
	}

	err = json.NewDecoder(resp.body).Decode(&accels)
	ensureReaderClosed(resp)
	return accels, err
}
