package client

import (
	"encoding/json"

	"github.com/docker/engine-api/types"
	"golang.org/x/net/context"
)

// AccelCreate creates a volume in the docker host.
func (cli *Client) AccelCreate(ctx context.Context, options types.AccelCreateRequest) (types.Accel, error) {
	var accel types.Accel
	resp, err := cli.post(ctx, "/accelerators/slots/create", nil, options, nil)
	if err != nil {
		return accel, err
	}
	err = json.NewDecoder(resp.body).Decode(&accel)
	ensureReaderClosed(resp)
	return accel, err
}
