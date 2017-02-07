// +build !windows

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/libaccelerator/driverapi"
	remoteapi "github.com/docker/docker/libaccelerator/drivers/remote/api"
	"github.com/docker/docker/pkg/integration/checker"
	"github.com/docker/engine-api/types"
	"github.com/go-check/check"
)

const dummyAccelDriver = "dummy-accel-driver"
const fakeRuntime = "fakeruntime:1.0"

var deviceInfo = driverapi.DeviceInfo{
	SupportedRuntimes: []string{fakeRuntime},
	DeviceIdentify:    "DummyDevice",
	Capacity:          make(map[string]string),
	Status:            "available",
}

var accelDevice = types.AccelDevice{
	SupportedRuntimes: deviceInfo.SupportedRuntimes,
	DeviceIdentify:    deviceInfo.DeviceIdentify,
	Capacity:          deviceInfo.Capacity,
	Driver:            dummyAccelDriver,
	Status:            deviceInfo.Status,
}

func init() {
	check.Suite(&DockerAccelSuite{
		ds: &DockerSuite{},
	})
}

type DockerAccelSuite struct {
	server *httptest.Server
	ds     *DockerSuite
	d      *Daemon
}

func (s *DockerAccelSuite) TearDownTest(c *check.C) {
	// call DockerSuite.TearDownTest() to cleanup **error** remained
	// things (e.g. container, image, volume, network, ...)
	s.ds.TearDownTest(c)

	// cleanup our acceleraters
	out, _, err := dockerCmdWithError("accel", "ls", "--format", "{{.ID}}")
	out = strings.TrimSpace(out)
	if err == nil && out != "" {
		for _, slot := range strings.Split(out, "\n") {
			dockerCmdWithError("accel", "rm", slot)
		}
	}
}

func (s *DockerAccelSuite) SetUpSuite(c *check.C) {
	s.d = NewDaemon(c)

	mux := http.NewServeMux()
	s.server = httptest.NewServer(mux)
	c.Assert(s.server, check.NotNil, check.Commentf("Failed to start an HTTP Server"))
	setupRemoteAccelDrivers(c, mux, s.server.URL, dummyAccelDriver)
}

func (s *DockerAccelSuite) TearDownSuite(c *check.C) {
	if s.server == nil {
		return
	}

	s.server.Close()

	cleanupRemoteAccelDrivers(c, dummyAccelDriver)
	err := os.RemoveAll("/etc/docker/plugins")
	c.Assert(err, checker.IsNil)
	os.RemoveAll("/fakeAccelSource")
}

type remoteAccelDriver struct {
	runtime string
	slots   map[string]slot
}

type slot struct {
	name    string
	runtime string
	device  string
	options []string
}

var fakeDriver = remoteAccelDriver{
	runtime: fakeRuntime,
	slots:   make(map[string]slot),
}

func cleanupRemoteAccelDrivers(c *check.C, accelDrv string) {
	fileName := fmt.Sprintf("/etc/docker/plugins/%s.spec", accelDrv)
	err := os.RemoveAll(fileName)
	c.Assert(err, checker.IsNil)
}

func setupRemoteAccelDrivers(c *check.C, mux *http.ServeMux, url, accelDrv string) {
	mux.HandleFunc("/Plugin.Activate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		fmt.Fprintf(w, `{"Implements":["%s"]}`, driverapi.AcceleratorPluginEndpointType)
	})

	mux.HandleFunc(fmt.Sprintf("/%s.GetCapability", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")

		gcr := remoteapi.GetCapabilityResponse{}
		gcr.Runtimes = append(gcr.Runtimes, fakeRuntime)

		ls, err := json.Marshal(gcr)
		if err != nil {
			http.Error(w, "Json Marshal capability error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, string(ls))
	})

	mux.HandleFunc(fmt.Sprintf("/%s.GetRuntime", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")

		gcr := remoteapi.GetRuntimesResponse{}
		gcr.Runtimes = append(gcr.Runtimes, fakeRuntime)

		ls, err := json.Marshal(gcr)
		if err != nil {
			http.Error(w, "Json Marshal runtimes error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, string(ls))
	})

	mux.HandleFunc(fmt.Sprintf("/%s.QueryRuntime", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		var qsRequest remoteapi.QueryRuntimeRequest
		err := json.NewDecoder(r.Body).Decode(&qsRequest)
		if err != nil {
			http.Error(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		if qsRequest.Runtime != fakeDriver.runtime {
			fmt.Fprintf(w, `{"Error":"specified runtime %s not support"}`, qsRequest.Runtime)
		} else {
			fmt.Fprintf(w, "null")
		}
	})

	mux.HandleFunc(fmt.Sprintf("/%s.ListDevice", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		resp := remoteapi.ListDeviceResponse{
			Devices: []driverapi.DeviceInfo{deviceInfo},
		}

		jsonResp, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, "Json Marshal slotInfo error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		fmt.Fprintf(w, string(jsonResp))
	})

	mux.HandleFunc(fmt.Sprintf("/%s.AllocateSlot", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		var asRequest remoteapi.AllocateSlotRequest
		err := json.NewDecoder(r.Body).Decode(&asRequest)
		if err != nil {
			http.Error(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		fakeDriver.slots[asRequest.SlotID] = slot{
			name:    asRequest.SlotID,
			runtime: asRequest.Runtime,
			options: asRequest.Options,
		}

		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		fmt.Fprintf(w, "null")
	})

	mux.HandleFunc(fmt.Sprintf("/%s.ReleaseSlot", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		var rsRequest remoteapi.ReleaseSlotRequest
		err := json.NewDecoder(r.Body).Decode(&rsRequest)
		if err != nil {
			http.Error(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		if _, ok := fakeDriver.slots[rsRequest.SlotID]; !ok {
			fmt.Fprintf(w, `{"Error":"slot %s not found"}`, rsRequest.SlotID)
		} else {
			delete(fakeDriver.slots, rsRequest.SlotID)
			fmt.Fprintf(w, "null")
		}
	})

	mux.HandleFunc(fmt.Sprintf("/%s.ListSlot", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		var sl []string
		for name, _ := range fakeDriver.slots {
			sl = append(sl, name)
		}

		ls, err := json.Marshal(sl)
		if err != nil {
			http.Error(w, "Json Marshal slots error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		fmt.Fprintf(w, string(ls))
	})

	mux.HandleFunc(fmt.Sprintf("/%s.SlotInfo", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		var siRequest remoteapi.SlotInfoRequest
		err := json.NewDecoder(r.Body).Decode(&siRequest)
		if err != nil {
			http.Error(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		slot, ok := fakeDriver.slots[siRequest.SlotID]
		if !ok {
			fmt.Fprintf(w, `{"Error":"slot %s not found"}`, siRequest.SlotID)
			return
		}

		siResponse := remoteapi.SlotInfoResponse{
			SlotInfo: driverapi.SlotInfo{
				Name:    slot.name,
				Device:  slot.device,
				Runtime: slot.runtime,
			},
		}

		si, err := json.Marshal(siResponse)
		if err != nil {
			http.Error(w, "Json Marshal slotInfo error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, string(si))
	})

	mux.HandleFunc(fmt.Sprintf("/%s.PrepareSlot", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		var psRequest remoteapi.PrepareSlotRequest
		err := json.NewDecoder(r.Body).Decode(&psRequest)
		if err != nil {
			http.Error(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		slot, ok := fakeDriver.slots[psRequest.SlotID]
		if !ok {
			fmt.Fprintf(w, `{"Error":"slot %s not found"}`, psRequest.SlotID)
			return
		}

		slot.device = "/dev/zero"
		sConfig := driverapi.SlotConfig{
			Envs: make(map[string]string),
		}
		os.MkdirAll("/fakeAccelSource", 0755)

		sConfig.Binds = append(sConfig.Binds, driverapi.Mount{
			Source:      "/fakeAccelSource",
			Destination: "/fakeDestination",
			Mode:        "rw",
		})
		sConfig.Devices = append(sConfig.Devices, slot.device)
		sConfig.Envs["fakeEnv"] = "fakeEnv"

		psResponse := remoteapi.PrepareSlotResponse{
			SlotConfig: sConfig,
		}

		ps, err := json.Marshal(psResponse)
		if err != nil {
			http.Error(w, "Json Marshal prepare slot repsonse error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, string(ps))
	})

	err := os.MkdirAll("/etc/docker/plugins", 0755)
	c.Assert(err, check.IsNil)

	fileName := fmt.Sprintf("/etc/docker/plugins/%s.spec", accelDrv)
	err = ioutil.WriteFile(fileName, []byte(url), 0644)
	c.Assert(err, check.IsNil)
}

func assertAccelIsAvailable(c *check.C, name string) {
	out, _ := dockerCmd(c, "accel", "inspect", "--format", "{{.Name}}", name)
	c.Assert(strings.TrimSpace(out), check.Equals, "/"+name)
}

func assertAccelNotAvailable(c *check.C, name string) {
	out, _, err := dockerCmdWithError("accel", "inspect", name)
	c.Assert(err, check.NotNil)
	c.Assert(string(out), checker.Contains, "No such accel")
}

func getAccelResource(c *check.C, name string) *types.Accel {
	out, _ := dockerCmd(c, "accel", "inspect", name)

	nr := []types.Accel{}
	err := json.Unmarshal([]byte(out), &nr)
	c.Assert(err, check.IsNil)

	return &nr[0]
}

func (s *DockerAccelSuite) TestDockerAccelDrivers(c *check.C) {
	out, _ := dockerCmd(c, "accel", "drivers")

	c.Assert(out, checker.Contains, dummyAccelDriver)
	c.Assert(out, checker.Contains, "DummyDevice")
}

func (s *DockerAccelSuite) TestDockerAccelLs(c *check.C) {
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "accells0")
	defer dockerCmd(c, "accel", "rm", "accells0")
	assertAccelIsAvailable(c, "accells0")
}

func (s *DockerAccelSuite) TestDockerAccelLsFormat(c *check.C) {
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "accellsformat0")
	defer dockerCmd(c, "accel", "rm", "accellsformat0")

	out, _ := dockerCmd(c, "accel", "ls", "--format", "{{.Name}}")
	c.Assert(strings.TrimSpace(out), checker.DeepEquals, "accellsformat0")
}

func (s *DockerAccelSuite) TestDockerAccelLsFilter(c *check.C) {
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "dev0")
	out, _ := dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "dev1")
	accelID := strings.TrimSpace(out)
	defer func() {
		dockerCmd(c, "accel", "rm", "dev0")
		dockerCmd(c, "accel", "rm", "dev1")
	}()

	filterChecker := func(filter string, accels ...string) {
		out, _ = dockerCmd(c, "accel", "ls", "--format", "{{.Name}}", "-f", filter)
		c.Assert(strings.Split(strings.TrimSpace(out), "\n"), check.DeepEquals, accels)
	}

	filterChecker("id="+accelID[0:5], "dev1") // filter with partial ID
	filterChecker("name=ev", "dev0", "dev1")
	filterChecker("name=dev1", "dev1")
	filterChecker("scope=container", "")
	filterChecker("scope=global", "dev0", "dev1")
	filterChecker("driver=nosuchdriver", "")
	filterChecker("driver="+dummyAccelDriver, "dev0", "dev1")
}

func (s *DockerAccelSuite) TestDockerAccelCreateOnly(c *check.C) {
	out, _ := dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testaccel")
	accelID := strings.TrimSpace(out)
	assertAccelIsAvailable(c, "testaccel")

	out, _ = dockerCmd(c, "accel", "inspect", "--format", "{{.ID}}", "testaccel")
	c.Assert(strings.TrimSpace(out), check.Equals, accelID)
}

func (s *DockerAccelSuite) TestDockerAccelCreateWithOption(c *check.C) {
	out, _ := dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "--option", "opA", "--option", "opB", "testaccel")
	slotID := strings.TrimSpace(out)
	defer dockerCmd(c, "accel", "rm", slotID)

	c.Assert(fakeDriver.slots[slotID].options, check.DeepEquals, []string{"opA", "opB"})

	out, _ = dockerCmd(c, "accel", "inspect", "--format", "{{.Options}}", slotID)
	c.Assert(strings.TrimSpace(out), check.Equals, "[opA opB]")
}

func (s *DockerAccelSuite) TestDockerAccelCreateDelete(c *check.C) {
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testdelete")
	assertAccelIsAvailable(c, "testdelete")

	dockerCmd(c, "accel", "rm", "testdelete")
	assertAccelNotAvailable(c, "testdelete")
}

func (s *DockerAccelSuite) TestDockerAccelDeleteNotExists(c *check.C) {
	assertAccelNotAvailable(c, "notexist")
	out, _, err := dockerCmdWithError("accel", "rm", "notexist")
	c.Assert(err, checker.NotNil, check.Commentf(out))
}

func (s *DockerAccelSuite) TestDockerAccelDeleteMultiple(c *check.C) {
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testdelmulti0")
	assertAccelIsAvailable(c, "testdelmulti0")

	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testdelmulti1")
	assertAccelIsAvailable(c, "testdelmulti1")

	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testdelmulti2")
	assertAccelIsAvailable(c, "testdelmulti2")

	// delete three accelerators at the same time
	dockerCmd(c, "accel", "rm", "testdelmulti0", "testdelmulti1", "testdelmulti2")

	assertAccelNotAvailable(c, "testdelmulti0")
	assertAccelNotAvailable(c, "testdelmulti1")
	assertAccelNotAvailable(c, "testdelmulti2")
}

func (s *DockerAccelSuite) TestDockerAccelInspect(c *check.C) {
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testinspect")
	defer func() {
		dockerCmd(c, "accel", "rm", "testinspect")
	}()
	assertAccelIsAvailable(c, "testinspect")

	out, _ := dockerCmd(c, "accel", "inspect", "--format={{ .Name }}", "testinspect")
	c.Assert(strings.TrimSpace(out), check.Equals, "/testinspect")

	accels := []types.Accel{}
	out, _ = dockerCmd(c, "accel", "inspect", "testinspect")
	err := json.Unmarshal([]byte(out), &accels)
	c.Assert(err, check.IsNil)
	c.Assert(accels, checker.HasLen, 1)
	c.Assert(accels[0].State, checker.Equals, "free")
	c.Assert(accels[0].Owner, checker.Equals, "")
}

func (s *DockerAccelSuite) TestDockerAccelDirectInspect(c *check.C) {
	out, _ := dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testinspectdirectly")
	defer func() {
		dockerCmd(c, "accel", "rm", "testinspectdirectly")
	}()
	assertAccelIsAvailable(c, "testinspectdirectly")
	accelID := strings.TrimSpace(out)

	accels := []types.Accel{}
	out, _ = dockerCmd(c, "inspect", "testinspectdirectly")
	err := json.Unmarshal([]byte(out), &accels)
	c.Assert(err, check.IsNil)
	c.Assert(accels, checker.HasLen, 1)

	out, _ = dockerCmd(c, "inspect", "--format={{ .Name }}", "testinspectdirectly")
	c.Assert(strings.TrimSpace(out), checker.Equals, "/testinspectdirectly")

	out, _ = dockerCmd(c, "inspect", "--format={{ .Name }}", accelID)
	c.Assert(strings.TrimSpace(out), checker.Equals, "/testinspectdirectly")

	out, _, err = dockerCmdWithError("inspect", "--format={{ .Name }}", "nosuchaccel")
	c.Assert(err, check.NotNil)
	c.Assert(strings.TrimSpace(out), checker.Contains, "No such image or container or accelerator")
}

func (s *DockerAccelSuite) TestDockerAccelInspectWithID(c *check.C) {
	out, _ := dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "test2")
	defer func() {
		dockerCmd(c, "accel", "rm", "test2")
	}()
	accelID := strings.TrimSpace(out)
	assertAccelIsAvailable(c, "test2")

	out, _ = dockerCmd(c, "accel", "inspect", "--format={{ .ID }}", "test2")
	c.Assert(strings.TrimSpace(out), check.Equals, accelID)
}

func (s *DockerAccelSuite) TestDockerAccelInspectMultipleAccel(c *check.C) {
	out, _ := dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "test1")
	test1ID := strings.TrimSpace(out)
	assertAccelIsAvailable(c, "test1")

	out, _ = dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "test2")
	test2ID := strings.TrimSpace(out)
	assertAccelIsAvailable(c, "test2")
	defer func() {
		dockerCmd(c, "accel", "rm", "test1", "test2")
	}()

	out, _ = dockerCmd(c, "accel", "inspect", "test1", "test2")

	accelResources := []types.Accel{}
	c.Assert(out, checker.Contains, test1ID)
	c.Assert(out, checker.Contains, test2ID)

	err := json.Unmarshal([]byte(out), &accelResources)
	c.Assert(err, check.IsNil)
	c.Assert(accelResources, checker.HasLen, 2)

	out, _, err = dockerCmdWithError("accel", "inspect", "test1", "nonexistent")
	c.Assert(err, check.NotNil)
	c.Assert(string(out), checker.Contains, "No such accel: nonexistent")
	c.Assert(out, checker.Contains, test1ID)

	// Should print an error and return an exitCode, nothing else
	out, _, err = dockerCmdWithError("accel", "inspect", "nonexistent")
	c.Assert(err, check.NotNil)
	c.Assert(string(out), checker.Contains, "No such accel: nonexistent")
}

func (s *DockerAccelSuite) TestDockerAccelInspectCustomUnspecified(c *check.C) {
	out, _ := dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "accelicu1")
	accelID := strings.TrimSpace(out)
	assertAccelIsAvailable(c, "accelicu1")

	nr := getAccelResource(c, "accelicu1")
	c.Assert(nr.Driver, checker.Equals, dummyAccelDriver)
	c.Assert(nr.Scope, checker.Equals, "global")
	c.Assert(nr.ID, checker.Equals, accelID)
	c.Assert(nr.Name, checker.Equals, "/accelicu1")
	c.Assert(nr.Runtime, checker.Equals, fakeRuntime)
	c.Assert(nr.Owner, checker.Equals, "")

	dockerCmd(c, "accel", "rm", "accelicu1")
	assertAccelNotAvailable(c, "accelicu1")
}

func (s *DockerAccelSuite) TestDockerAccelCreateDeleteSpecialCharacters(c *check.C) {
	_, _, err := dockerCmdWithError("accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "test@#$")
	c.Assert(err, checker.NotNil)
	_, _, err = dockerCmdWithError("accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "kiwl$%^")
	c.Assert(err, checker.NotNil)
}

func (s *DockerAccelSuite) TestDockerAccelBuildImage(c *check.C) {
	name := "testbuildaccelimage"
	label := "slot0=" + fakeRuntime + ";slot1=" + fakeRuntime

	// build from scratch
	if _, err := buildImage(name, `
                FROM scratch
                LABEL runtime `+label,
		true); err != nil {
		c.Fatal(err)
	}
	out, _ := dockerCmd(c, "inspect", "--format", " {{.ContainerConfig.Labels.runtime}}", name)
	c.Assert(strings.TrimSpace(out), check.Equals, label)
	deleteImages(name)

	// build from busybox
	if _, err := buildImage(name, `
                FROM busybox
                LABEL runtime `+label,
		true); err != nil {
		c.Fatal(err)
	}
	out, _ = dockerCmd(c, "inspect", "--format", " {{.ContainerConfig.Labels.runtime}}", name)
	c.Assert(strings.TrimSpace(out), check.Equals, label)
	deleteImages(name)
}

func (s *DockerAccelSuite) TestDockerAccelImplictCreateInRun(c *check.C) {
	// prepare image
	name := "testbuildaccelimage"
	label := "slot0=" + fakeRuntime + ";slot1=" + fakeRuntime
	_, err := buildImage(name, `
                FROM busybox
                LABEL runtime `+label,
		true)
	c.Assert(err, check.IsNil)
	defer deleteImages(name)

	// run test
	out, _ := dockerCmd(c, "run", "-d", name, "top")
	contID := strings.TrimSpace(out)
	defer dockerCmd(c, "rm", "-f", contID)

	out, _ = dockerCmd(c, "accel", "ls", "--format", "{{.ID}}", "--filter", "Owner="+contID)
	slots := strings.Split(strings.TrimSpace(string(out)), "\n")
	c.Assert(slots, checker.HasLen, 2)
}
func (s *DockerAccelSuite) TestDockerAccelCreateWithoutSpecifyDriver(c *check.C) {
	_, err := dockerCmd(c, "accel", "create", "--runtime", fakeRuntime, "nodrivername")
	c.Assert(err, check.NotNil)
	defer dockerCmd(c, "accel", "rm", "nodrivername")
}

func (s *DockerAccelSuite) TestDockerAccelCreateSpecifyDriverFirst(c *check.C) {
	_, err := dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "accelcsdf1")
	defer func() {
		dockerCmd(c, "accel", "rm", "-f", "accelcsdf1")
	}()
	c.Assert(err, check.Equals, 0)

	_, err = dockerCmd(c, "accel", "create", "--runtime", fakeRuntime, "accelcsdf2")
	defer func() {
		dockerCmd(c, "accel", "rm", "-f", "accelcsdf2")
	}()
	c.Assert(err, check.Equals, 0)
}

func (s *DockerAccelSuite) TestDockerAccelCreateWithDriverPluginSet(c *check.C) {
	err := s.d.StartWithBusybox("--accel-driver-plugin", dummyAccelDriver)
	c.Assert(err, check.IsNil)

	_, err = s.d.Cmd("accel", "create", "--runtime", fakeRuntime, "accelcwdps1")
	c.Assert(err, check.IsNil)

	_, err = s.d.Cmd("accel", "create", "--runtime", fakeRuntime, "accelcwdps2")
	c.Assert(err, check.IsNil)

	s.d.Cmd("accel", "rm", "accelcwdps1")
	s.d.Cmd("accel", "rm", "accelcwdps2")
	s.d.Stop()
}

// waitRun will wait for the specified container to be running, maximum 5 seconds.
func (s *DockerAccelSuite) waitRun(name string) error {
	expr := "{{.State.Running}}"
	expected := "true"
	after := time.After(5)

	for {
		result, err := s.d.Cmd("inspect", "-f", expr, name)
		if err != nil {
			if !strings.Contains(err.Error(), "No such") {
				return fmt.Errorf("error executing docker inspect: %s:%s", string(result), err.Error())
			}
			select {
			case <-after:
				return err
			default:
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}

		out := strings.TrimSpace(string(result))
		if out == expected {
			break
		}

		select {
		case <-after:
			return fmt.Errorf("condition \"%q == %q\" not true in time", out, expected)
		default:
		}

		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (s *DockerAccelSuite) getInspectBody(c *check.C, version, id string) (io.ReadCloser, error) {
	clientConfig, err := s.d.getClientConfig()
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: clientConfig.transport,
	}

	endpoint := fmt.Sprintf("/%s/containers/%s/json", version, id)
	req, err := http.NewRequest("GET", endpoint, nil)
	c.Assert(err, check.IsNil, check.Commentf("[%s] could not create new request", id))
	req.URL.Host = clientConfig.addr
	req.URL.Scheme = clientConfig.scheme
	resp, err := client.Do(req)

	c.Assert(err, check.IsNil)
	c.Assert(resp.StatusCode, check.Equals, http.StatusOK)
	return resp.Body, nil
}
