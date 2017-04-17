package client

import (
	"fmt"

	"golang.org/x/net/context"

	Cli "github.com/docker/docker/cli"
	"github.com/docker/docker/opts"
	flag "github.com/docker/docker/pkg/mflag"
	"github.com/docker/docker/reference"
	"github.com/docker/engine-api/types"
)

// CmdCombine combines some partial images to one complete image.
//
// Usage: docker combine [options] IMAGE
func (cli *DockerCli) CmdCombine(args ...string) error {
	cmd := Cli.Subcmd("combine", []string{"IMAGE"}, Cli.DockerCommands["combine"].Description, true)
	flTags := opts.NewListOpts(validateTag)
	cmd.Var(&flTags, []string{"t", "-tag"}, "Name and optionally a tag in the 'name:tag' format")
	cmd.Require(flag.Exact, 1)
	cmd.ParseFlags(args, true)

	addTrustedFlags(cmd, true)

	image := cmd.Arg(0)
	_, err := reference.ParseNamed(image)
	if err != nil {
		return err
	}

	options := types.ImageCombineOptions{
		Tags:  flTags.GetAll(),
		Image: image,
	}

	response, err := cli.client.ImageCombine(context.Background(), options)
	if err != nil {
		return err
	}

	fmt.Fprintf(cli.out, "Image ID: %v\n", response.ImageID)

	if len(flTags.GetAll()) == 0 {
		fmt.Fprintf(cli.out, "Image Tag: %v\n", response.DefaultTag)
	}

	return nil
}
