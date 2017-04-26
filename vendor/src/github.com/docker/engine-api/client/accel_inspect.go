package client

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/docker/engine-api/types"
	"golang.org/x/net/context"
)

// AccelInspect returns the information about a specific accelerator in the docker host.
func (cli *Client) AccelInspect(ctx context.Context, name string) (types.Accel, error) {
	accelResource, _, err := cli.AccelInspectWithRaw(ctx, name, false)
	return accelResource, err
}

// AccelInspectWithRaw returns the information about a specific accelerator in the docker host and its raw representation.
func (cli *Client) AccelInspectWithRaw(ctx context.Context, name string, getSize bool) (types.Accel, []byte, error) {
	var accel types.Accel
	resp, err := cli.get(ctx, "/accelerators/slots/"+name, nil, nil)
	if err != nil {
		if resp.statusCode == http.StatusNotFound {
			return accel, nil, accelNotFoundError{name}
		}
		return accel, nil, err
	}
	defer ensureReaderClosed(resp)

	body, err := ioutil.ReadAll(resp.body)
	if err != nil {
		return accel, nil, err
	}
	rdr := bytes.NewReader(body)
	err = json.NewDecoder(rdr).Decode(&accel)
	return accel, body, err
}
