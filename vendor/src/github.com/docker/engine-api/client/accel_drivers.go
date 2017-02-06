package client

import (
	"encoding/json"

	"github.com/docker/engine-api/types"
	"golang.org/x/net/context"
)

// AccelDriversList returns the accelerators configured in the docker host.
func (cli *Client) AccelDriversList(ctx context.Context) (types.AccelDriversResponse, error) {
	var accelDrivers types.AccelDriversResponse

	resp, err := cli.get(ctx, "/accelerators/drivers", nil, nil)
	if err != nil {
		return accelDrivers, err
	}

	err = json.NewDecoder(resp.body).Decode(&accelDrivers)
	ensureReaderClosed(resp)
	return accelDrivers, err
}
