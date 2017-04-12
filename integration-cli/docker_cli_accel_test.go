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
	"github.com/docker/engine-api/types/versions/v1p20"
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
	// rollback control flag
	fakeDriver.unmarkNoService()
	fakeDriver.unmarkNotFound()

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

	// control flags for test
	isNoService bool // if set, AllocateSlot call will reture NoService error
	isNotFound  bool // if set, Slot call will return NotFound error
}

func (d *remoteAccelDriver) markNoService() {
	d.isNoService = true
}
func (d *remoteAccelDriver) unmarkNoService() {
	d.isNoService = false
}
func (d *remoteAccelDriver) markNotFound() {
	d.isNotFound = true
}
func (d *remoteAccelDriver) unmarkNotFound() {
	d.isNotFound = false
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

	isNoService: false,
	isNotFound:  false,
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
		request := remoteapi.Request{
			Args: &remoteapi.QueryRuntimeRequest{},
		}
		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			http.Error(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		qsRequest, ok := request.Args.(*remoteapi.QueryRuntimeRequest)
		if !ok {
			http.Error(w, "Unable to decode QueryRuntimeRequest: "+err.Error(), http.StatusBadRequest)
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
		request := remoteapi.Request{
			Args: &remoteapi.AllocateSlotRequest{},
		}
		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			http.Error(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		asRequest, ok := request.Args.(*remoteapi.AllocateSlotRequest)
		if !ok {
			http.Error(w, "Unable to decode AllocateSlotRequest: "+err.Error(), http.StatusBadRequest)
			return
		}

		resp := remoteapi.AllocateSlotResponse{}
		resp.ErrType = remoteapi.RESP_ERR_NOERROR
		resp.ErrMsg = ""
		// if NoService flags set, return error
		if fakeDriver.isNoService {
			resp.ErrType = remoteapi.RESP_ERR_NODEV
			resp.ErrMsg = asRequest.Runtime
		} else {
			// else, allocate a slot
			fakeDriver.slots[asRequest.SlotID] = slot{
				name:    asRequest.SlotID,
				runtime: asRequest.Runtime,
				options: asRequest.Options,
			}
		}

		jsonResp, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, "Json Marshal slotInfo error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		fmt.Fprintf(w, string(jsonResp))
	})

	mux.HandleFunc(fmt.Sprintf("/%s.ReleaseSlot", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		request := remoteapi.Request{
			Args: &remoteapi.ReleaseSlotRequest{},
		}
		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			http.Error(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		rsRequest, ok := request.Args.(*remoteapi.ReleaseSlotRequest)
		if !ok {
			http.Error(w, "Unable to decode ReleaseSlotRequest: "+err.Error(), http.StatusBadRequest)
			return
		}

		resp := remoteapi.ReleaseSlotResponse{}
		resp.ErrType = remoteapi.RESP_ERR_NOERROR
		resp.ErrMsg = ""
		if _, ok := fakeDriver.slots[rsRequest.SlotID]; !ok || fakeDriver.isNotFound {
			resp.ErrType = remoteapi.RESP_ERR_NOTFOUND
			resp.ErrMsg = fmt.Sprintf("slot %s not found", rsRequest.SlotID)
		} else {
			delete(fakeDriver.slots, rsRequest.SlotID)
		}

		jsonResp, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, "Json Marshal slotInfo error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		fmt.Fprintf(w, string(jsonResp))
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
		request := remoteapi.Request{
			Args: &remoteapi.SlotInfoRequest{},
		}
		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			http.Error(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		siRequest, ok := request.Args.(*remoteapi.SlotInfoRequest)
		if !ok {
			http.Error(w, "Unable to decode SlotInfoRequest: "+err.Error(), http.StatusBadRequest)
			return
		}

		resp := remoteapi.SlotInfoResponse{}
		resp.ErrType = remoteapi.RESP_ERR_NOERROR
		resp.ErrMsg = ""

		slot, ok := fakeDriver.slots[siRequest.SlotID]
		if !ok || fakeDriver.isNotFound {
			resp.ErrType = remoteapi.RESP_ERR_NOTFOUND
			resp.ErrMsg = fmt.Sprintf("slot %s not found", siRequest.SlotID)
		} else {
			resp.SlotInfo.Name = slot.name
			resp.SlotInfo.Device = slot.device
			resp.SlotInfo.Runtime = slot.runtime
		}

		jsonResp, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, "Json Marshal slotInfo error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		fmt.Fprintf(w, string(jsonResp))
	})

	mux.HandleFunc(fmt.Sprintf("/%s.PrepareSlot", driverapi.AcceleratorPluginEndpointType), func(w http.ResponseWriter, r *http.Request) {
		request := remoteapi.Request{
			Args: &remoteapi.PrepareSlotRequest{},
		}
		err := json.NewDecoder(r.Body).Decode(&request)
		if err != nil {
			http.Error(w, "Unable to decode JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		psRequest, ok := request.Args.(*remoteapi.PrepareSlotRequest)
		if !ok {
			http.Error(w, "Unable to decode PrepareSlotRequest: "+err.Error(), http.StatusBadRequest)
			return
		}

		resp := remoteapi.PrepareSlotResponse{}
		resp.ErrType = remoteapi.RESP_ERR_NOERROR
		resp.ErrMsg = ""
		slot, ok := fakeDriver.slots[psRequest.SlotID]
		if !ok || fakeDriver.isNotFound {
			resp.ErrType = remoteapi.RESP_ERR_NOTFOUND
			resp.ErrMsg = fmt.Sprintf("slot %s not found", psRequest.SlotID)
		} else {
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

			resp.SlotConfig = sConfig
		}

		jsonResp, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, "Json Marshal prepare slot repsonse error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.docker.plugins.v1+json")
		fmt.Fprintf(w, string(jsonResp))
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
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testaccel")
	assertAccelIsAvailable(c, "testaccel")

	out, _ := dockerCmd(c, "run", "-d", "--accel", "testaccel", "busybox", "top")
	contID := strings.TrimSpace(out)
	defer func() {
		dockerCmd(c, "rm", "-f", contID)
		dockerCmd(c, "accel", "rm", "testaccel")
	}()

	c.Assert(waitRun(contID), checker.IsNil)
	out, _ = dockerCmd(c, "accel", "inspect", "--format", "{{.Owner}}", "testaccel")
	c.Assert(strings.TrimSpace(out), check.Equals, contID)
}

func (s *DockerAccelSuite) TestDockerAccelCreateWithOption(c *check.C) {
	out, _ := dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "--option", "opA", "--option", "opB", "testaccel")
	slotID := strings.TrimSpace(out)
	defer dockerCmd(c, "accel", "rm", slotID)

	c.Assert(fakeDriver.slots[slotID].options, check.DeepEquals, []string{"opA", "opB"})

	out, _ = dockerCmd(c, "accel", "inspect", "--format", "{{.Options}}", slotID)
	c.Assert(strings.TrimSpace(out), check.Equals, "[opA opB]")
}

func (s *DockerAccelSuite) TestDockerAccelCreateInRun(c *check.C) {
	out, _ := dockerCmd(c, "run", "-d", "--accel", "slot0="+fakeRuntime+"@"+dummyAccelDriver, "busybox", "top")
	contID := strings.TrimSpace(out)
	defer dockerCmd(c, "rm", "-f", contID)

	out, _ = dockerCmd(c, "accel", "ls", "--format", "{{.ID}}")
	name := strings.Split(strings.TrimSpace(string(out)), "\n")
	c.Assert(name, checker.HasLen, 1)

	verifyContainerHasAccelerators(c, contID, name)

	out, _ = dockerCmd(c, "accel", "inspect", "--format", "{{.Scope}}", name[0])
	c.Assert(strings.TrimSpace(out), check.Equals, "container")
}

func (s *DockerAccelSuite) TestDockerAccelCreateInRunWithOption(c *check.C) {
	out, _ := dockerCmd(c, "create", "--accel", "slot0="+fakeRuntime+"@"+dummyAccelDriver+",opA,opB", "busybox", "top")
	contID := strings.TrimSpace(out)
	defer dockerCmd(c, "rm", "-f", contID)

	out, _ = dockerCmd(c, "inspect", "--format", "{{(index .HostConfig.Accelerators 0).Sid}}", contID)
	slotID := strings.TrimSpace(out)

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

func (s *DockerAccelSuite) TestDockerAccelDeleteInUse(c *check.C) {
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testdeleteinuse")
	assertAccelIsAvailable(c, "testdeleteinuse")

	out, _ := dockerCmd(c, "run", "-d", "--accel", "testdeleteinuse", "busybox", "top")
	id := strings.TrimSpace(out)
	defer func() {
		dockerCmd(c, "rm", "-f", id)
		dockerCmd(c, "accel", "rm", "testdeleteinuse")
	}()

	out, _, err := dockerCmdWithError("accel", "rm", "testdeleteinuse")
	c.Assert(err, checker.NotNil, check.Commentf("%v", out))
	c.Assert(out, checker.Contains, "slot in use")
}

func (s *DockerAccelSuite) TestDockerAccelDeleteMultiple(c *check.C) {
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testdelmulti0")
	assertAccelIsAvailable(c, "testdelmulti0")

	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testdelmulti1")
	assertAccelIsAvailable(c, "testdelmulti1")

	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "testdelmulti2")
	assertAccelIsAvailable(c, "testdelmulti2")

	out, _ := dockerCmd(c, "run", "-d", "--accel", "testdelmulti2", "busybox", "top")
	contID := strings.TrimSpace(out)
	defer func() {
		dockerCmd(c, "rm", "-f", contID)
		dockerCmd(c, "accel", "rm", "testdelmulti2")
	}()

	waitRun(contID)

	// delete three accelerators at the same time, since testdelmulti2
	// contains active container, its deletion should fail.
	out, _, err := dockerCmdWithError("accel", "rm", "testdelmulti0", "testdelmulti1", "testdelmulti2")
	// err should not be nil due to deleting testdelmulti2 failed.
	c.Assert(err, checker.NotNil, check.Commentf("out: %s", out))
	// testdelmulti2 should fail due to slot in use
	c.Assert(out, checker.Contains, "slot in use")

	assertAccelNotAvailable(c, "testdelmulti0")
	assertAccelNotAvailable(c, "testdelmulti1")

	// testDelMulti2 can't be deleted, so it should exist
	assertAccelIsAvailable(c, "testdelmulti2")
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
	c.Assert(accels[0].InjectInfo, checker.DeepEquals, types.AccelInject{})

	// create a container using this accel
	out, _ = dockerCmd(c, "create", "-ti", "--accel", "testinspect", "busybox", "top")
	contID := strings.TrimSpace(out)
	defer dockerCmd(c, "rm", "-f", contID)

	// check State, Owner, InjectInfo
	out, _ = dockerCmd(c, "accel", "inspect", "testinspect")
	err = json.Unmarshal([]byte(out), &accels)
	c.Assert(err, check.IsNil)
	c.Assert(accels, checker.HasLen, 1)
	c.Assert(accels[0].State, checker.Equals, "used")
	c.Assert(accels[0].Owner, checker.Equals, contID)
	c.Assert(accels[0].InjectInfo, checker.DeepEquals, types.AccelInject{})

	// start the container and check InjectInfo
	dockerCmd(c, "start", contID)
	out, _ = dockerCmd(c, "accel", "inspect", "testinspect")
	err = json.Unmarshal([]byte(out), &accels)
	c.Assert(err, check.IsNil)
	c.Assert(accels, checker.HasLen, 1)
	c.Assert(accels[0].InjectInfo.Bindings, checker.NotNil)
	c.Assert(accels[0].InjectInfo.Devices, checker.NotNil)
	c.Assert(accels[0].InjectInfo.Environments, checker.NotNil)
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

func (s *DockerAccelSuite) TestDockerAccelInspectAccelWithContainerName(c *check.C) {
	dockerCmd(c, "accel", "create", "--runtime", fakeRuntime, "--driver", dummyAccelDriver, "accelforinspect")
	assertAccelIsAvailable(c, "accelforinspect")

	out, _ := dockerCmd(c, "run", "-d", "--name", "testAccelInspect1", "--accel", "accelforinspect", "busybox", "top")
	contID := strings.TrimSpace(out)
	defer func() {
		// we don't stop container by name, because we'll rename it later
		dockerCmd(c, "rm", "-f", contID)
		dockerCmd(c, "accel", "rm", "accelforinspect")
	}()
	c.Assert(waitRun("testAccelInspect1"), check.IsNil)

	accelResources := []types.Accel{}
	out, _ = dockerCmd(c, "accel", "inspect", "accelforinspect")
	err := json.Unmarshal([]byte(out), &accelResources)
	c.Assert(err, check.IsNil)
	c.Assert(accelResources, checker.HasLen, 1)
	container := accelResources[0].Owner
	c.Assert(container, checker.Equals, contID)
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

func (s *DockerAccelSuite) TestDockerAccelDriverUngracefulRestart(c *check.C) {
	// launch new accel driver plugin: dad
	dad := "dad"
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	setupRemoteAccelDrivers(c, mux, server.URL, dad)

	// start docker daemon with proper accel-driver set
	s.d.StartWithBusybox("--accel-driver-plugin", dad)

	out, err := s.d.Cmd("accel", "create", "-d", dad, "--runtime", fakeRuntime, "accel1")
	c.Assert(err, checker.IsNil)
	accelId := strings.TrimSpace(out)

	out, err = s.d.Cmd("run", "-itd", "--accel", "accel1", "--name", "foo", "busybox", "sh")
	c.Assert(err, checker.IsNil)
	contID := strings.TrimSpace(out)

	// Kill daemon and restart
	if err = s.d.Stop(); err != nil {
		c.Fatal(err)
	}
	// kill accel driver plugin
	server.Close()

	// restart accel driver
	mux = http.NewServeMux()
	server = httptest.NewServer(mux)
	setupRemoteAccelDrivers(c, mux, server.URL, dad)

	startTime := time.Now().Unix()
	if err = s.d.Restart(); err != nil {
		c.Fatal(err)
	}

	lapse := time.Now().Unix() - startTime
	if lapse > 60 {
		// In normal scenarios, daemon restart takes ~1 second.
		// Plugin retry mechanism can delay the daemon start. systemd may not like it.
		// Avoid accessing plugins during daemon bootup
		c.Logf("daemon restart took too long : %d seconds", lapse)
	}

	// trying to reuse the same slot
	out, err = s.d.Cmd("start", contID)
	c.Assert(err, checker.IsNil)

	out, err = s.d.Cmd("accel", "inspect", "--format", "{{.Owner}}", accelId)
	c.Assert(err, checker.IsNil)
	c.Assert(strings.TrimSpace(out), checker.Equals, contID)

	s.d.Cmd("rm", "-f", "foo")
	s.d.Cmd("accel", "rm", "accel1")
	s.d.Stop()
	server.Close()
	cleanupRemoteAccelDrivers(c, dad)
}

func (s *DockerAccelSuite) TestDockerAccelInspectApiMultipleAccels(c *check.C) {
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "accelinspect1")
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "accelinspect2")

	out, _ := dockerCmd(c, "run", "-d", "--accel=accelinspect1", "--accel=accelinspect2", "busybox", "top")
	id := strings.TrimSpace(out)
	defer func() {
		dockerCmd(c, "rm", "-f", id)
		dockerCmd(c, "accel", "rm", "accelinspect1")
		dockerCmd(c, "accel", "rm", "accelinspect2")
	}()
	c.Assert(waitRun(id), check.IsNil)

	var inspect120 v1p20.ContainerJSON
	body := getInspectBody(c, "v1.20", id)
	err := json.Unmarshal(body, &inspect120)
	c.Assert(err, checker.IsNil)
	c.Assert(inspect120.HostConfig.Accelerators, checker.HasLen, 2)

	var inspect121 types.ContainerJSON
	body = getInspectBody(c, "v1.21", id)
	err = json.Unmarshal(body, &inspect121)
	c.Assert(err, checker.IsNil)
	c.Assert(inspect121.HostConfig.Accelerators, checker.HasLen, 2)

	accel1 := inspect120.HostConfig.Accelerators[0]
	c.Assert(accel1.Runtime, checker.Equals, fakeRuntime)
	c.Assert(accel1.Driver, checker.Equals, dummyAccelDriver)

	accel2 := inspect120.HostConfig.Accelerators[1]
	c.Assert(accel2.Runtime, checker.Equals, fakeRuntime)
	c.Assert(accel2.Driver, checker.Equals, dummyAccelDriver)

	accel3 := inspect121.HostConfig.Accelerators[0]
	c.Assert(accel3.Runtime, checker.Equals, fakeRuntime)
	c.Assert(accel3.Driver, checker.Equals, dummyAccelDriver)

	accel4 := inspect121.HostConfig.Accelerators[1]
	c.Assert(accel4.Runtime, checker.Equals, fakeRuntime)
	c.Assert(accel4.Driver, checker.Equals, dummyAccelDriver)
}

func verifyContainerHasAccelerators(c *check.C, cName string, accels []string) {
	// Verify container contains all the accelerators
	var aIDs []string
	for _, accel := range accels {
		out, _ := dockerCmd(c, "accel", "inspect", "-f", "{{.ID}}", accel)
		accelID := strings.TrimSpace(out)
		aIDs = append(aIDs, accelID)
	}

	var inspect types.ContainerJSON
	body := getInspectBody(c, "v1.21", cName)
	err := json.Unmarshal(body, &inspect)
	c.Assert(err, checker.IsNil, check.Commentf("failed to unmarshal inspect result"))
	c.Assert(inspect, check.NotNil, check.Commentf("container inspect get nil result"))

	var ids []string
	for _, acc := range inspect.HostConfig.Accelerators {
		ids = append(ids, acc.Sid)
	}
	c.Assert(ids, check.NotNil, check.Commentf("container do not have accelerators"))

	idstr := strings.Join(ids, ",")
	for _, aid := range aIDs {
		c.Assert(idstr, checker.Contains, aid)
	}
}

func (s *DockerAccelSuite) verifyContainerHasAccelerators(c *check.C, cName string, accels []string) {
	// Verify container contains all the accelerators
	var aIDs []string
	for _, accel := range accels {
		out, _ := s.d.Cmd("accel", "inspect", "-f", "{{.ID}}", accel)
		accelID := strings.TrimSpace(out)
		aIDs = append(aIDs, accelID)
	}

	body, err := s.getInspectBody(c, "v1.21", cName)
	c.Assert(err, checker.IsNil)

	var inspect types.ContainerJSON
	err = json.NewDecoder(body).Decode(&inspect)
	c.Assert(err, checker.IsNil, check.Commentf("failed to unmarshal inspect result"))
	c.Assert(inspect, check.NotNil, check.Commentf("container inspect get nil result"))

	var ids []string
	for _, acc := range inspect.HostConfig.Accelerators {
		ids = append(ids, acc.Sid)
	}
	c.Assert(ids, check.NotNil, check.Commentf("container do not have accelerators"))

	idstr := strings.Join(ids, ",")
	for _, aid := range aIDs {
		c.Assert(idstr, checker.Contains, aid)
	}
}

func (s *DockerAccelSuite) TestDockerAccelMultipleAcceleratorsGracefulDaemonRestart(c *check.C) {
	s.d.StartWithBusybox("--accel-driver-plugin", dummyAccelDriver)
	s.d.Cmd("accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "accelgdr1")
	s.d.Cmd("accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "accelgdr2")

	out, _ := s.d.Cmd("run", "-d", "--accel=accelgdr1", "--accel=accelgdr2", "busybox", "top")
	id := strings.TrimSpace(out)
	c.Assert(s.waitRun(id), check.IsNil)

	accels := []string{"accelgdr1", "accelgdr2"}
	s.verifyContainerHasAccelerators(c, id, accels)

	// Reload daemon
	s.d.Restart()

	s.d.Cmd("start", id)

	s.verifyContainerHasAccelerators(c, id, accels)
	s.d.Cmd("rm", "-f", id)
	s.d.Cmd("accel", "rm", "accelgdr1")
	s.d.Cmd("accel", "rm", "accelgdr2")
	s.d.Stop()
}

func (s *DockerAccelSuite) TestDockerAccelMultipleAccelsUngracefulDaemonRestart(c *check.C) {
	err := s.d.StartWithBusybox("--accel-driver-plugin", dummyAccelDriver)
	c.Assert(err, check.IsNil)

	accels := []string{"acceludr1", "acceludr2"}

	_, err = s.d.Cmd("accel", "create", "--runtime", fakeRuntime, "acceludr1")
	c.Assert(err, check.IsNil)
	out, err := s.d.Cmd("accel", "create", "--runtime", fakeRuntime, "acceludr2")
	c.Assert(err, check.IsNil)
	out, err = s.d.Cmd("run", "-d", "--accel=acceludr1", "--accel=acceludr2", "busybox", "top")
	c.Assert(err, check.IsNil)
	id := strings.TrimSpace(out)
	c.Assert(s.waitRun(id), check.IsNil)

	s.verifyContainerHasAccelerators(c, id, accels)
	// Reload daemon
	if err := s.d.Stop(); err != nil {
		c.Fatalf("Could not kill daemon: %v", err)
	}
	s.d.Restart()
	s.d.Cmd("start", id)
	s.verifyContainerHasAccelerators(c, id, accels)

	s.d.Cmd("rm", "-f", id)
	s.d.Cmd("accel", "rm", "acceludr1")
	s.d.Cmd("accel", "rm", "acceludr2")
	s.d.Stop()
}

func (s *DockerAccelSuite) TestDockerAccelRunByID(c *check.C) {
	out, _ := dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "one")
	sid := strings.TrimSpace(out)
	out, _ = dockerCmd(c, "run", "-d", "--accel", sid, "busybox", "top")
	id := strings.TrimSpace(out)
	defer func() {
		dockerCmd(c, "rm", "-f", id)
		dockerCmd(c, "accel", "rm", "one")
	}()
}

func (s *DockerAccelSuite) TestDockerAccelCreateDeleteSpecialCharacters(c *check.C) {
	_, _, err := dockerCmdWithError("accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "test@#$")
	c.Assert(err, checker.NotNil)
	_, _, err = dockerCmdWithError("accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "kiwl$%^")
	c.Assert(err, checker.NotNil)
}

func (s *DockerAccelSuite) TestDockerAccelUpdate(c *check.C) {
	out, _ := dockerCmd(c, "run", "-d", "--accel", "slot0="+fakeRuntime, "busybox", "top")
	contID := strings.TrimSpace(out)

	out, _ = dockerCmd(c, "accel", "ls", "--format", "{{.ID}}")
	name := strings.Split(strings.TrimSpace(string(out)), "\n")
	c.Assert(name, checker.HasLen, 1)

	verifyContainerHasAccelerators(c, contID, name)

	out, _ = dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "accelupdate")
	accelID := strings.TrimSpace(out)

	// try to update slot0 while container is running
	out, _, err := dockerCmdWithError("update", "--accel", "slot0=accelupdate", contID)
	c.Assert(err, checker.NotNil)
	c.Assert(out, checker.Contains, "Container is running")

	//try update slot0 with container stopped
	dockerCmd(c, "stop", contID)
	dockerCmd(c, "update", "--accel", "slot0=accelupdate", contID)
	verifyContainerHasAccelerators(c, contID, []string{accelID})

	// try to add a new slot
	out, _ = dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "accelupdate1")
	accelID1 := strings.TrimSpace(out)

	dockerCmd(c, "update", "--accel", "slot1=accelupdate1", contID)
	verifyContainerHasAccelerators(c, contID, []string{accelID, accelID1})

	// try to start container after accel update
	dockerCmd(c, "start", contID)

	dockerCmd(c, "rm", "-f", contID)
	dockerCmd(c, "accel", "rm", accelID, accelID1)
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

func (s *DockerAccelSuite) TestDockerAccelCreateInRunBinding(c *check.C) {
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
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "s0")
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "s1")
	out, _ := dockerCmd(c, "run", "-d", "--accel", "slot0=s0", "--accel", "slot1=s1", name, "top")
	contID := strings.TrimSpace(out)
	defer func() {
		dockerCmd(c, "rm", "-f", contID)
		dockerCmd(c, "accel", "rm", "s0")
		dockerCmd(c, "accel", "rm", "s1")
	}()

	out, _ = dockerCmd(c, "accel", "ls", "--format", "{{.Name}}", "--filter", "Owner="+contID)
	slots := strings.Split(strings.TrimSpace(string(out)), "\n")
	c.Assert(slots, checker.DeepEquals, []string{"s0", "s1"})
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
