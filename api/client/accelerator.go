package client

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"golang.org/x/net/context"

	"github.com/docker/docker/api/client/formatter"
	Cli "github.com/docker/docker/cli"
	"github.com/docker/docker/libaccelerator"
	"github.com/docker/docker/opts"
	flag "github.com/docker/docker/pkg/mflag"
	runconfigopts "github.com/docker/docker/runconfig/opts"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/filters"
)

// CmdAccel is the parent subcommand for all accel commands
// Usage: docker accel <COMMAND> [OPTIONS]
func (cli *DockerCli) CmdAccel(args ...string) error {
	cmd := Cli.Subcmd("accel", []string{"COMMAND [OPTIONS]"}, accelUsage(), false)
	cmd.Require(flag.Min, 1)
	err := cmd.ParseFlags(args, true)
	cmd.Usage()
	return err
}

// CmdAccelCreate creates a new accel with a given name
// Usage: docker accel create [OPTIONS] <ACCEL-NAME>
func (cli *DockerCli) CmdAccelCreate(args ...string) error {
	cmd := Cli.Subcmd("accel create", []string{"ACCEL-NAME"}, "Creates a new accel with a name specified by the user", false)

	flOpts := opts.NewListOpts(nil)
	cmd.Var(&flOpts, []string{"o", "-option"}, "Specify options for accelerator driver")
	flDriver := cmd.String([]string{"d", "-driver"}, "", "Specify accelerator driver name")
	flRuntime := cmd.String([]string{"r", "-runtime"}, "", "Specify the runtime of the accelerator")

	cmd.Require(flag.Max, 1)
	err := cmd.ParseFlags(args, true)
	if err != nil {
		return err
	}

	if len(*flRuntime) == 0 {
		return fmt.Errorf("accelerator runtime is required.")
	}
	if len(*flDriver) == 0 && flOpts.Len() != 0 {
		return fmt.Errorf("accelerator options must be specified along with driver.\n")
	}

	// Construct accel create request body
	accelReq := types.AccelCreateRequest{
		Driver:  *flDriver,
		Runtime: *flRuntime,
		Options: flOpts.GetAllOrEmpty(),
	}
	if cmd.NArg() == 1 {
		accelReq.Name = cmd.Arg(0)
	} else {
		accelReq.Name = ""
	}
	// validate runtime and name
	if !runconfigopts.ValidateAccelRuntime(accelReq.Runtime) {
		return fmt.Errorf("Invalid accelerator runtime: %s\n", accelReq.Runtime)
	} else if accelReq.Name != "" && !runconfigopts.ValidateAccelName(accelReq.Name) {
		return fmt.Errorf("Invalid accelerator name: %s\n", accelReq.Name)
	}

	resp, err := cli.client.AccelCreate(context.Background(), accelReq)
	if err != nil {
		return err
	}
	fmt.Fprintf(cli.out, "%s\n", resp.ID)
	return nil
}

// CmdAccelRm deletes one or more accels
// Usage: docker accel rm ACCEL-NAME|ACCEL-ID [ACCEL-NAME|ACCEL-ID...]
func (cli *DockerCli) CmdAccelRm(args ...string) error {
	cmd := Cli.Subcmd("accel rm", []string{"ACCEL [ACCEL...]"}, "Deletes one or more accelerators", false)
	cmd.Require(flag.Min, 1)
	flForce := cmd.Bool([]string{"f", "-force"}, false, "Always remove accelerators")
	if err := cmd.ParseFlags(args, true); err != nil {
		return err
	}

	status := 0
	for _, accl := range cmd.Args() {
		accl = strings.Trim(accl, "/")
		if err := cli.client.AccelRemove(context.Background(), accl, *flForce); err != nil {
			fmt.Fprintf(cli.err, "%s\n", err)
			status = 1
			continue
		}
	}

	if status != 0 {
		return Cli.StatusError{StatusCode: status}
	}
	return nil
}

type byAccelName []*types.Accel

func (r byAccelName) Len() int      { return len(r) }
func (r byAccelName) Swap(i, j int) { r[i], r[j] = r[j], r[i] }
func (r byAccelName) Less(i, j int) bool {
	if r[i].BadDriver != r[j].BadDriver {
		return !r[i].BadDriver
	} else if r[i].NoDevice != r[j].NoDevice {
		return !r[i].NoDevice
	} else if r[i].Scope == r[j].Scope {
		return (r[i].Name < r[j].Name)
	} else {
		return (r[i].Scope == libaccelerator.GlobalScope)
	}
}

// CmdAccelLs lists all the accels managed by docker daemon
// Usage: docker accel ls [OPTIONS]
func (cli *DockerCli) CmdAccelLs(args ...string) error {
	cmd := Cli.Subcmd("accel ls", nil, "Lists accelerators", true)

	flQuiet := cmd.Bool([]string{"q", "-quiet"}, false, "Only display numeric names")
	flFormat := cmd.String([]string{"-format"}, "", "Pretty-print accelerators using a Go template")
	flNoTrunc := cmd.Bool([]string{"-no-trunc"}, false, "Show accelerator complete information")
	flFilter := opts.NewListOpts(nil)
	cmd.Var(&flFilter, []string{"f", "-filter"}, "Filter output based on conditions provided")

	cmd.Require(flag.Exact, 0)
	err := cmd.ParseFlags(args, true)
	if err != nil {
		return err
	}

	// Consolidate all filter flags, and sanity check them early.
	// They'll get process after get response from server.
	accFilterArgs := filters.NewArgs()
	for _, f := range flFilter.GetAll() {
		if accFilterArgs, err = filters.ParseFlag(f, accFilterArgs); err != nil {
			return err
		}
	}

	accelResources, err := cli.client.AccelList(context.Background(), accFilterArgs)
	if err != nil {
		return err
	}

	f := *flFormat
	if len(f) == 0 {
		f = "table"
	}
	sort.Sort(byAccelName(accelResources.Accels))

	accelCtx := formatter.AccelContext{
		Context: formatter.Context{
			Output: cli.out,
			Format: f,
			Quiet:  *flQuiet,
			Trunc:  !*flNoTrunc,
		},
		Accels: accelResources.Accels,
	}

	accelCtx.Write()
	return nil
}

// CmdAccelInspect inspects the accel object for more details
// Usage: docker accel inspect [OPTIONS] <ACCEL> [ACCEL...]
func (cli *DockerCli) CmdAccelInspect(args ...string) error {
	cmd := Cli.Subcmd("accel inspect", []string{"ACCEL [ACCEL...]"}, "Displays detailed information on one or more accels", false)
	flFormat := cmd.String([]string{"f", "-format"}, "", "Format the output using the given go template")
	cmd.Require(flag.Min, 1)

	if err := cmd.ParseFlags(args, true); err != nil {
		return err
	}

	inspectSearcher := func(name string) (interface{}, []byte, error) {
		i, err := cli.client.AccelInspect(context.Background(), name)
		return i, nil, err
	}

	return cli.inspectElements(*flFormat, cmd.Args(), inspectSearcher)
}

// CmdAccelDrivers list all the accelerator drivers engine support
// Usage: docker accel drivers
func (cli *DockerCli) CmdAccelDrivers(args ...string) error {
	cmd := Cli.Subcmd("accel drivers", []string{}, "Displays all the accelerator drivers engine support", false)
	cmd.Require(flag.Exact, 0)

	if err := cmd.ParseFlags(args, true); err != nil {
		return err
	}

	accDrivers, err := cli.client.AccelDriversList(context.Background())
	if err != nil {
		return err
	}

	wr := tabwriter.NewWriter(cli.out, 8, 1, 3, ' ', 0)

	// unless quiet (-q) is specified, print field titles
	for _, drv := range accDrivers.Drivers {
		lstDesc := strings.Split(strings.TrimSpace(drv.Desc), "\n")
		fmt.Fprintf(wr, "\nDriver:\n\t%s\nDescription:\n\t%s\n",
			drv.Name, strings.Join(lstDesc, "\n\t"))
		fmt.Fprint(wr, "\n")
	}
	wr.Flush()
	return nil

}

func accelUsage() string {
	accelCommands := map[string]string{
		"create":  "Create a accel",
		"inspect": "Display detailed accel information",
		"ls":      "List all accels",
		"rm":      "Remove a accel",
		"drivers": "Show all the drivers engine supported",
	}

	help := "Commands:\n"

	for cmd, description := range accelCommands {
		help += fmt.Sprintf("  %-25.25s%s\n", cmd, description)
	}

	help += fmt.Sprintf("\nRun 'docker accel COMMAND --help' for more information on a command.")
	return help
}
