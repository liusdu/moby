package client

import (
	"net/url"

	"golang.org/x/net/context"
)

// AccelRemove removes a accel from the docker host.
func (cli *Client) AccelRemove(ctx context.Context, name string, force bool) error {
	query := url.Values{}
	if force {
		query.Set("force", "1")
	}
	resp, err := cli.delete(ctx, "/accelerators/slots/"+name, query, nil)
	ensureReaderClosed(resp)
	return err
}
