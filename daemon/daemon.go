// Package daemon exposes the functions that occur on the host server
// that the Docker daemon is running.
//
// In implementing the various functions of the daemon, there is often
// a method-specific struct for configuring the runtime behavior.
package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	containerd "github.com/docker/containerd/api/grpc/types"
	"github.com/docker/docker/api"
	"github.com/docker/docker/builder"
	"github.com/docker/docker/container"
	"github.com/docker/docker/daemon/events"
	"github.com/docker/docker/daemon/exec"
	"github.com/docker/docker/errors"
	"github.com/docker/engine-api/types"
	containertypes "github.com/docker/engine-api/types/container"
	eventtypes "github.com/docker/engine-api/types/events"
	"github.com/docker/engine-api/types/filters"
	networktypes "github.com/docker/engine-api/types/network"
	registrytypes "github.com/docker/engine-api/types/registry"
	"github.com/docker/engine-api/types/strslice"
	// register graph drivers
	_ "github.com/docker/docker/daemon/graphdriver/register"
	"github.com/docker/docker/distribution"
	dmetadata "github.com/docker/docker/distribution/metadata"
	"github.com/docker/docker/distribution/xfer"
	"github.com/docker/docker/dockerversion"
	"github.com/docker/docker/image"
	"github.com/docker/docker/image/tarexport"
	"github.com/docker/docker/layer"
	"github.com/docker/docker/libaccelerator"
	"github.com/docker/docker/libcontainerd"
	"github.com/docker/docker/migrate/v1"
	"github.com/docker/docker/pkg/fileutils"
	"github.com/docker/docker/pkg/graphdb"
	"github.com/docker/docker/pkg/idtools"
	"github.com/docker/docker/pkg/progress"
	"github.com/docker/docker/pkg/registrar"
	"github.com/docker/docker/pkg/signal"
	"github.com/docker/docker/pkg/streamformatter"
	"github.com/docker/docker/pkg/sysinfo"
	"github.com/docker/docker/pkg/system"
	"github.com/docker/docker/pkg/truncindex"
	"github.com/docker/docker/reference"
	"github.com/docker/docker/registry"
	"github.com/docker/docker/runconfig"
	"github.com/docker/docker/utils"
	volumedrivers "github.com/docker/docker/volume/drivers"
	"github.com/docker/docker/volume/local"
	"github.com/docker/docker/volume/store"
	"github.com/docker/go-connections/nat"
	"github.com/docker/libnetwork"
	nwconfig "github.com/docker/libnetwork/config"
	"github.com/docker/libtrust"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/net/context"
)

const (
	// maxDownloadConcurrency is the maximum number of downloads that
	// may take place at a time for each pull.
	maxDownloadConcurrency = 3
	// maxUploadConcurrency is the maximum number of uploads that
	// may take place at a time for each push.
	maxUploadConcurrency = 5
)

var (
	errSystemNotSupported = fmt.Errorf("The Docker daemon is not supported on this platform.")
)

// ErrImageDoesNotExist is error returned when no image can be found for a reference.
type ErrImageDoesNotExist struct {
	RefOrID string
}

func (e ErrImageDoesNotExist) Error() string {
	return fmt.Sprintf("no such id: %s", e.RefOrID)
}

// Daemon holds information about the Docker daemon.
type Daemon struct {
	ID                        string
	repository                string
	containers                container.Store
	execCommands              *exec.Store
	referenceStore            reference.Store
	downloadManager           *xfer.LayerDownloadManager
	uploadManager             *xfer.LayerUploadManager
	distributionMetadataStore dmetadata.Store
	trustKey                  libtrust.PrivateKey
	idIndex                   *truncindex.TruncIndex
	configStore               *Config
	statsCollector            *statsCollector
	defaultLogConfig          containertypes.LogConfig
	RegistryService           *registry.Service
	EventsService             *events.Events
	netController             libnetwork.NetworkController
	accelController           libaccelerator.AcceleratorController
	volumes                   *store.VolumeStore
	discoveryWatcher          discoveryReloader
	root                      string
	hookStore                 string
	seccompEnabled            bool
	shutdown                  bool
	uidMaps                   []idtools.IDMap
	gidMaps                   []idtools.IDMap
	layerStore                layer.Store
	imageStore                image.Store
	nameIndex                 *registrar.Registrar
	linkIndex                 *linkIndex
	containerd                libcontainerd.Client
	containerdRemote          libcontainerd.Remote
	defaultIsolation          containertypes.Isolation // Default isolation mode on Windows
	Hooks                     specs.Hooks
	hosts                     map[string]bool // hosts stores the addresses the daemon is listening on
}

// StoreHosts stores the addresses the daemon is listening on
func (daemon *Daemon) StoreHosts(hosts []string) {
	if daemon.hosts == nil {
		daemon.hosts = make(map[string]bool)
	}
	for _, h := range hosts {
		daemon.hosts[h] = true
	}
}

func (daemon *Daemon) cleanupRedundantContainers(containers map[string]*container.Container, containerdCons map[string]*containerd.Container) map[string]*container.Container {
	res := make(map[string]*container.Container)
	for dcid, dcont := range containerdCons {
		cont, ok := containers[dcid]
		if ok {
			//container that in containerd and not stopped need restore
			res[dcid] = cont
		} else { // kill containers running in containerd but not exist in docker
			logrus.Debugf("remove redundant containerd container: %s", dcid)
			if err := daemon.containerd.Signal(dcid, int(syscall.SIGKILL)); err != nil {
				logrus.Warnf("kill container %s error: %v", dcid, err)
			}
			if err := system.EnsureRemoveAll(dcont.BundlePath); err != nil {
				logrus.Warnf("remove %s error: %v", dcont.BundlePath, err)
			}
		}
	}
	return res
}

func (daemon *Daemon) restore() error {
	var (
		debug         = utils.IsDebugEnabled()
		currentDriver = daemon.GraphDriverName()
		containers    = make(map[string]*container.Container)
	)

	if !debug {
		logrus.Info("Loading containers: start.")
	}
	dir, err := ioutil.ReadDir(daemon.repository)
	if err != nil {
		return err
	}

	for _, v := range dir {
		id := v.Name()
		container, err := daemon.load(id)
		if !debug && logrus.GetLevel() == logrus.InfoLevel {
			fmt.Print(".")
		}
		if err != nil {
			logrus.Errorf("Failed to load container %v: %v", id, err)
			continue
		}

		// Ignore the container if it does not support the current driver being used by the graph
		if (container.Driver == "" && currentDriver == "aufs") || container.Driver == currentDriver {
			rwlayer, err := daemon.layerStore.GetRWLayer(container.ID)
			if err != nil {
				logrus.Errorf("Failed to load container mount %v: %v", id, err)
				continue
			}
			container.RWLayer = rwlayer
			logrus.Debugf("Loaded container %v", container.ID)

			containers[container.ID] = container
		} else {
			logrus.Debugf("Cannot load container %s because it was created with another graph driver.", container.ID)
		}
	}

	var migrateLegacyLinks bool
	restartContainers := make(map[*container.Container]chan struct{})
	activeSandboxes := make(map[string]interface{})
	for _, c := range containers {
		if err := daemon.registerName(c); err != nil {
			logrus.Errorf("Failed to register container %s: %s", c.ID, err)
			continue
		}
		if err := daemon.Register(c); err != nil {
			logrus.Errorf("Failed to register container %s: %s", c.ID, err)
			continue
		}
	}

	//check starting status and clean
	var containerNeedRestore map[string]*container.Container
	if containerdContainers, err := daemon.containerd.GetRunningContainerdContainers(); err == nil {
		containerNeedRestore = daemon.cleanupRedundantContainers(containers, containerdContainers)
	} else {
		logrus.Debugf("get containerd containers error:%v", err)
		containerNeedRestore = make(map[string]*container.Container)
	}

	var wg sync.WaitGroup
	var mapLock sync.Mutex
	for _, c := range containers {
		wg.Add(1)
		go func(c *container.Container) {
			defer wg.Done()
			_, ok := containerNeedRestore[c.ID]
			if c.IsRunning() || c.IsPaused() || ok {
				c.RestartManager().Cancel() // manually start containers because some need to wait for swarm networking
				if err := daemon.containerd.Restore(c.ID, c.InitializeStdio); err != nil {
					logrus.Errorf("Failed to restore %s with containerd: %s", c.ID, err)
					return
				}

				// we call Mount and then Unmount to get BaseFs of the container
				if err := daemon.Mount(c); err != nil {
					// The mount is unlikely to fail. However, in case mount fails
					// the container should be allowed to restore here. Some functionalities
					// (like docker exec -u user) might be missing but container is able to be
					// stopped/restarted/removed.
					// See #29365 for related information.
					// The error is only logged here.
					logrus.Warnf("Failed to mount container on getting BaseFs path %v: %v", c.ID, err)
				} else {
					if err := daemon.Unmount(c); err != nil {
						logrus.Warnf("Failed to umount container on getting BaseFs path %v: %v", c.ID, err)
					}

				}

				c.ResetRestartManager(false)
				if !c.HostConfig.NetworkMode.IsContainer() && c.IsRunning() {
					options, err := daemon.buildSandboxOptions(c)
					if err != nil {
						logrus.Warnf("Failed build sandbox option to restore container %s: %v", c.ID, err)
					}
					mapLock.Lock()
					activeSandboxes[c.NetworkSettings.SandboxID] = options
					mapLock.Unlock()
				}
			}
			// fixme: only if not running
			// get list of containers we need to restart
			if daemon.configStore.AutoRestart && !c.IsRunning() && !c.IsPaused() && c.ShouldRestart() {
				mapLock.Lock()
				restartContainers[c] = make(chan struct{})
				mapLock.Unlock()
			}

			if c.RemovalInProgress {
				// We probably crashed in the middle of a removal, reset
				// the flag.
				//
				// We DO NOT remove the container here as we do not
				// know if the user had requested for either the
				// associated volumes, network links or both to also
				// be removed. So we put the container in the "dead"
				// state and leave further processing up to them.
				logrus.Debugf("Resetting RemovalInProgress flag from %v", c.ID)
				c.ResetRemovalInProgress()
				c.SetDead()
				c.ToDisk()
			}

			// if c.hostConfig.Links is nil (not just empty), then it is using the old sqlite links and needs to be migrated
			if c.HostConfig != nil && c.HostConfig.Links == nil {
				migrateLegacyLinks = true
			}
		}(c)
	}
	wg.Wait()

	err = daemon.removeRedundantMounts(containers)
	if err != nil {
		// just print error info
		logrus.Errorf("removeRedundantMounts failed %v", err)
	}

	// get all active accel slots, for accelController initialization
	activeAccelSlots := make(map[string]string)
	for _, c := range containers {
		for _, accel := range c.HostConfig.Accelerators {
			if accel.Sid != "" {
				activeAccelSlots[accel.Sid] = c.ID
			} else {
				// Un-allocated persistent accelerator detected, which is caused by
				// daemon crash when calling plugin. We just warn user about this,
				// and will try to re-allocate resource when start this container
				if accel.IsPersistent {
					logrus.Warnf("container \"%s\": detected un-allocated persistent accelerator \"%s\", will try to re-allocate it when start.", c.Name, accel.Name)
				}
			}
		}
	}

	daemon.netController, err = daemon.initNetworkController(daemon.configStore, activeSandboxes)
	if err != nil {
		return fmt.Errorf("Error initializing network controller: %v", err)
	}

	// Create libaccelerator controller
	daemon.accelController, err = daemon.initAccelController(daemon.configStore, activeAccelSlots)
	if err != nil {
		return fmt.Errorf("Error initializing accelerator controller: %v", err)
	}

	// migrate any legacy links from sqlite
	linkdbFile := filepath.Join(daemon.root, "linkgraph.db")
	var legacyLinkDB *graphdb.Database
	if migrateLegacyLinks {
		legacyLinkDB, err = graphdb.NewSqliteConn(linkdbFile)
		if err != nil {
			return fmt.Errorf("error connecting to legacy link graph DB %s, container links may be lost: %v", linkdbFile, err)
		}
		defer legacyLinkDB.Close()
	}

	// Now that all the containers are registered, register the links
	for _, c := range containers {
		if migrateLegacyLinks {
			if err := daemon.migrateLegacySqliteLinks(legacyLinkDB, c); err != nil {
				return err
			}
		}
		if err := daemon.registerLinks(c, c.HostConfig); err != nil {
			logrus.Errorf("failed to register link for container %s: %v", c.ID, err)
		}
	}

	group := sync.WaitGroup{}
	for c, notifier := range restartContainers {
		group.Add(1)

		go func(c *container.Container, chNotify chan struct{}) {
			defer group.Done()

			logrus.Debugf("Starting container %s", c.ID)

			// ignore errors here as this is a best effort to wait for children to be
			//   running before we try to start the container
			children := daemon.children(c)
			timeout := time.After(5 * time.Second)
			for _, child := range children {
				if notifier, exists := restartContainers[child]; exists {
					select {
					case <-notifier:
					case <-timeout:
					}
				}
			}

			// Make sure networks are available before starting
			daemon.waitForNetworks(c)
			if err := daemon.containerStart(c, true); err != nil {
				logrus.Errorf("Failed to start container %s: %s", c.ID, err)
			}
			close(chNotify)
		}(c, notifier)

	}
	group.Wait()

	// any containers that were started above would already have had this done,
	// however we need to now prepare the mountpoints for the rest of the containers as well.
	// This shouldn't cause any issue running on the containers that already had this run.
	// This must be run after any containers with a restart policy so that containerized plugins
	// can have a chance to be running before we try to initialize them.
	for _, c := range containers {
		// if the container has restart policy, do not
		// prepare the mountpoints since it has been done on restarting.
		// This is to speed up the daemon start when a restart container
		// has a volume and the volume dirver is not available.
		if _, ok := restartContainers[c]; ok {
			continue
		}
		group.Add(1)
		go func(c *container.Container) {
			defer group.Done()
			if err := daemon.prepareMountPoints(c); err != nil {
				logrus.Error(err)
			}
		}(c)
	}

	group.Wait()

	if !debug {
		if logrus.GetLevel() == logrus.InfoLevel {
			fmt.Println()
		}
		logrus.Info("Loading containers: done.")
	}

	return nil
}

// waitForNetworks is used during daemon initialization when starting up containers
// It ensures that all of a container's networks are available before the daemon tries to start the container.
// In practice it just makes sure the discovery service is available for containers which use a network that require discovery.
func (daemon *Daemon) waitForNetworks(c *container.Container) {
	if daemon.discoveryWatcher == nil {
		return
	}
	// Make sure if the container has a network that requires discovery that the discovery service is available before starting
	for netName := range c.NetworkSettings.Networks {
		// If we get `ErrNoSuchNetwork` here, it can assumed that it is due to discovery not being ready
		// Most likely this is because the K/V store used for discovery is in a container and needs to be started
		if _, err := daemon.netController.NetworkByName(netName); err != nil {
			if _, ok := err.(libnetwork.ErrNoSuchNetwork); !ok {
				continue
			}
			// use a longish timeout here due to some slowdowns in libnetwork if the k/v store is on anything other than --net=host
			// FIXME: why is this slow???
			logrus.Debugf("Container %s waiting for network to be ready", c.Name)
			select {
			case <-daemon.discoveryWatcher.ReadyCh():
			case <-time.After(60 * time.Second):
			}
			return
		}
	}
}

func (daemon *Daemon) generateHostname(id string, config *containertypes.Config) {
	// Generate default hostname
	if config.Hostname == "" {
		config.Hostname = id[:12]
	}
}

func (daemon *Daemon) getEntrypointAndArgs(configEntrypoint strslice.StrSlice, configCmd strslice.StrSlice) (string, []string) {
	if len(configEntrypoint) != 0 {
		return configEntrypoint[0], append(configEntrypoint[1:], configCmd...)
	}
	return configCmd[0], configCmd[1:]
}

// SubscribeToEvents returns the currently record of events, a channel to stream new events from, and a function to cancel the stream of events.
func (daemon *Daemon) SubscribeToEvents(since, sinceNano int64, filter filters.Args) ([]eventtypes.Message, chan interface{}) {
	ef := events.NewFilter(filter)
	return daemon.EventsService.SubscribeTopic(since, sinceNano, ef)
}

// UnsubscribeFromEvents stops the event subscription for a client by closing the
// channel where the daemon sends events to.
func (daemon *Daemon) UnsubscribeFromEvents(listener chan interface{}) {
	daemon.EventsService.Evict(listener)
}

// GetLabels for a container or image id
func (daemon *Daemon) GetLabels(id string) map[string]string {
	// TODO: TestCase
	container := daemon.containers.Get(id)
	if container != nil {
		return container.Config.Labels
	}

	img, err := daemon.GetImage(id)
	if err == nil {
		return img.ContainerConfig.Labels
	}
	return nil
}

func (daemon *Daemon) children(c *container.Container) map[string]*container.Container {
	return daemon.linkIndex.children(c)
}

// parents returns the names of the parent containers of the container
// with the given name.
func (daemon *Daemon) parents(c *container.Container) map[string]*container.Container {
	return daemon.linkIndex.parents(c)
}

func (daemon *Daemon) registerLink(parent, child *container.Container, alias string) error {
	fullName := path.Join(parent.Name, alias)
	if err := daemon.nameIndex.Reserve(fullName, child.ID); err != nil {
		if err == registrar.ErrNameReserved {
			logrus.Warnf("error registering link for %s, to %s, as alias %s, ignoring: %v", parent.ID, child.ID, alias, err)
			return nil
		}
		return err
	}
	daemon.linkIndex.link(parent, child, fullName)
	return nil
}

// NewDaemon sets up everything for the daemon to be able to service
// requests from the webserver.
func NewDaemon(config *Config, registryService *registry.Service, containerdRemote libcontainerd.Remote) (daemon *Daemon, err error) {
	setDefaultMtu(config)

	// Ensure we have compatible and valid configuration options
	if err := verifyDaemonSettings(config); err != nil {
		return nil, err
	}

	// Do we have a disabled network?
	config.DisableBridge = isBridgeNetworkDisabled(config)

	// Verify the platform is supported as a daemon
	if !platformSupported {
		return nil, errSystemNotSupported
	}

	// Validate platform-specific requirements
	if err := checkSystem(); err != nil {
		return nil, err
	}

	// set up SIGUSR1 handler on Unix-like systems, or a Win32 global event
	// on Windows to dump Go routine stacks
	setupDumpStackTrap()

	uidMaps, gidMaps, err := setupRemappedRoot(config)
	if err != nil {
		return nil, err
	}
	rootUID, rootGID, err := idtools.GetRootUIDGID(uidMaps, gidMaps)
	if err != nil {
		return nil, err
	}

	// get the canonical path to the Docker root directory
	var realRoot string
	if _, err := os.Stat(config.Root); err != nil && os.IsNotExist(err) {
		realRoot = config.Root
	} else {
		realRoot, err = fileutils.ReadSymlinkedDirectory(config.Root)
		if err != nil {
			return nil, fmt.Errorf("Unable to get the full path to root (%s): %s", config.Root, err)
		}
	}

	if err = setupDaemonRoot(config, realRoot, rootUID, rootGID); err != nil {
		return nil, err
	}

	// set up the tmpDir to use a canonical path
	tmp, err := tempDir(config.Root, rootUID, rootGID)
	if err != nil {
		return nil, fmt.Errorf("Unable to get the TempDir under %s: %s", config.Root, err)
	}
	realTmp, err := fileutils.ReadSymlinkedDirectory(tmp)
	if err != nil {
		return nil, fmt.Errorf("Unable to get the full path to the TempDir (%s): %s", tmp, err)
	}
	os.Setenv("TMPDIR", realTmp)

	d := &Daemon{configStore: config}
	// Ensure the daemon is properly shutdown if there is a failure during
	// initialization
	defer func() {
		if err != nil {
			if err := d.Shutdown(); err != nil {
				logrus.Error(err)
			}
		}
	}()

	// Set the default isolation mode (only applicable on Windows)
	if err := d.setDefaultIsolation(); err != nil {
		return nil, fmt.Errorf("error setting default isolation mode: %v", err)
	}

	logrus.Debugf("Using default logging driver %s", config.LogConfig.Type)

	if err := configureMaxThreads(config); err != nil {
		logrus.Warnf("Failed to configure golang's threads limit: %v", err)
	}

	installDefaultAppArmorProfile()
	daemonRepo := filepath.Join(config.Root, "containers")
	if err := idtools.MkdirAllAs(daemonRepo, 0700, rootUID, rootGID); err != nil && !os.IsExist(err) {
		return nil, err
	}

	driverName := os.Getenv("DOCKER_DRIVER")
	if driverName == "" {
		driverName = config.GraphDriver
	}
	d.layerStore, err = layer.NewStoreFromOptions(layer.StoreOptions{
		StorePath:                 config.Root,
		MetadataStorePathTemplate: filepath.Join(config.Root, "image", "%s", "layerdb"),
		GraphDriver:               driverName,
		GraphDriverOptions:        config.GraphOptions,
		UIDMaps:                   uidMaps,
		GIDMaps:                   gidMaps,
	})
	if err != nil {
		return nil, err
	}

	graphDriver := d.layerStore.DriverName()
	imageRoot := filepath.Join(config.Root, "image", graphDriver)

	// Configure and validate the kernels security support
	if err := configureKernelSecuritySupport(config, graphDriver); err != nil {
		return nil, err
	}

	d.downloadManager = xfer.NewLayerDownloadManager(d.layerStore, maxDownloadConcurrency)
	d.uploadManager = xfer.NewLayerUploadManager(maxUploadConcurrency)

	ifs, err := image.NewFSStoreBackend(filepath.Join(imageRoot, "imagedb"))
	if err != nil {
		return nil, err
	}

	d.imageStore, err = image.NewImageStore(ifs, d.layerStore)
	if err != nil {
		return nil, err
	}

	// Configure the volumes driver
	volStore, err := configureVolumes(config, rootUID, rootGID)
	if err != nil {
		return nil, err
	}

	trustKey, err := api.LoadOrCreateTrustKey(config.TrustKeyPath)
	if err != nil {
		return nil, err
	}

	trustDir := filepath.Join(config.Root, "trust")

	if err := system.MkdirAll(trustDir, 0700); err != nil {
		return nil, err
	}

	distributionMetadataStore, err := dmetadata.NewFSMetadataStore(filepath.Join(imageRoot, "distribution"))
	if err != nil {
		return nil, err
	}

	eventsService := events.New()

	referenceStore, err := reference.NewReferenceStore(filepath.Join(imageRoot, "repositories.json"))
	if err != nil {
		return nil, fmt.Errorf("Couldn't create Tag store repositories: %s", err)
	}

	// delete reference of image not nornamlly loaded to imageStore
	for _, imageID := range referenceStore.List() {
		if img, err := d.imageStore.Get(imageID); err == nil {
			isExist := false
			if chainID := img.RootFS.ChainID(); chainID != "" {
				l, err := d.layerStore.Get(chainID)
				if err == nil {
					layer.ReleaseAndLog(d.layerStore, l)
					isExist = true
				}
			} else {
				isExist = true
			}
			// If the image not exist locally, delete its reference
			if !isExist {
				for _, ref := range referenceStore.References(imageID) {
					isDelete, err := referenceStore.Delete(ref)
					if isDelete {
						logrus.Warnf("Delete reference %s for image id %s from reference store", ref.String(), imageID)
					}
					if err != nil {
						logrus.Warnf("Faild to delete reference %s for image id %s: %v", ref.String(), imageID, err)
					}
				}
			}
		}
	}

	if err := restoreCustomImage(d.imageStore, d.layerStore, referenceStore); err != nil {
		return nil, fmt.Errorf("Couldn't restore custom images: %s", err)
	}

	migrationStart := time.Now()
	if err := v1.Migrate(config.Root, graphDriver, d.layerStore, d.imageStore, referenceStore, distributionMetadataStore); err != nil {
		logrus.Errorf("Graph migration failed: %q. Your old graph data was found to be too inconsistent for upgrading to content-addressable storage. Some of the old data was probably not upgraded. We recommend starting over with a clean storage directory if possible.", err)
	}
	logrus.Infof("Graph migration to content-addressability took %.2f seconds", time.Since(migrationStart).Seconds())

	// Discovery is only enabled when the daemon is launched with an address to advertise.  When
	// initialized, the daemon is registered and we can store the discovery backend as its read-only
	if err := d.initDiscovery(config); err != nil {
		return nil, err
	}

	sysInfo := sysinfo.New(false)
	// Check if Devices cgroup is mounted, it is hard requirement for container security,
	// on Linux/FreeBSD.
	if runtime.GOOS != "windows" && !sysInfo.CgroupDevicesEnabled {
		return nil, fmt.Errorf("Devices cgroup isn't mounted")
	}

	//setup hooks environment
	if err := d.initHooks(config, rootUID, rootGID); err != nil {
		return nil, fmt.Errorf("Failed to register default hooks: %v", err)
	}

	d.ID = trustKey.PublicKey().KeyID()
	d.repository = daemonRepo
	d.containers = container.NewMemoryStore()
	d.execCommands = exec.NewStore()
	d.referenceStore = referenceStore
	d.distributionMetadataStore = distributionMetadataStore
	d.trustKey = trustKey
	d.idIndex = truncindex.NewTruncIndex([]string{})
	d.statsCollector = d.newStatsCollector(1 * time.Second)
	d.defaultLogConfig = containertypes.LogConfig{
		Type:   config.LogConfig.Type,
		Config: config.LogConfig.Config,
	}
	d.RegistryService = registryService
	d.EventsService = eventsService
	d.volumes = volStore
	d.root = config.Root
	d.uidMaps = uidMaps
	d.gidMaps = gidMaps
	d.seccompEnabled = sysInfo.Seccomp

	d.nameIndex = registrar.NewRegistrar()
	d.linkIndex = newLinkIndex()
	d.containerdRemote = containerdRemote

	go d.execCommandGC()

	d.containerd, err = containerdRemote.Client(d)
	if err != nil {
		return nil, err
	}

	if err := d.restore(); err != nil {
		return nil, err
	}

	return d, nil
}

func (daemon *Daemon) shutdownContainer(c *container.Container) error {
	// TODO(windows): Handle docker restart with paused containers
	if c.IsPaused() {
		// To terminate a process in freezer cgroup, we should send
		// SIGTERM to this process then unfreeze it, and the process will
		// force to terminate immediately.
		logrus.Debugf("Found container %s is paused, sending SIGTERM before unpause it", c.ID)
		sig, ok := signal.SignalMap["TERM"]
		if !ok {
			return fmt.Errorf("System doesn not support SIGTERM")
		}
		if err := daemon.kill(c, int(sig)); err != nil {
			return fmt.Errorf("sending SIGTERM to container %s with error: %v", c.ID, err)
		}
		if err := daemon.containerUnpause(c); err != nil {
			return fmt.Errorf("Failed to unpause container %s with error: %v", c.ID, err)
		}
		if _, err := c.WaitStop(10 * time.Second); err != nil {
			logrus.Debugf("container %s failed to exit in 10 second of SIGTERM, sending SIGKILL to force", c.ID)
			sig, ok := signal.SignalMap["KILL"]
			if !ok {
				return fmt.Errorf("System does not support SIGKILL")
			}
			if err := daemon.kill(c, int(sig)); err != nil {
				logrus.Errorf("Failed to SIGKILL container %s", c.ID)
			}
			c.WaitStop(-1 * time.Second)
			return err
		}
	}
	// If container failed to exit in 10 seconds of SIGTERM, then using the force
	if err := daemon.containerStop(c, 10); err != nil {
		return fmt.Errorf("Stop container %s with error: %v", c.ID, err)
	}

	c.WaitStop(-1 * time.Second)
	return nil
}

// Shutdown stops the daemon.
func (daemon *Daemon) Shutdown() error {
	daemon.shutdown = true
	// Keep mounts and networking running on daemon shutdown if
	// we are to keep containers running and restore them.
	if daemon.configStore.LiveRestore && daemon.containers != nil {
		// check if there are any running containers, if none we should do some cleanup
		if ls, err := daemon.Containers(&types.ContainerListOptions{}); len(ls) != 0 || err != nil {
			return nil
		}
	}

	if daemon.containers != nil {
		logrus.Debug("starting clean shutdown of all containers...")
		daemon.containers.ApplyAll(func(c *container.Container) {
			if !c.IsRunning() {
				return
			}
			logrus.Debugf("stopping %s", c.ID)
			if err := daemon.shutdownContainer(c); err != nil {
				logrus.Errorf("Stop container error: %v", err)
				return
			}
			if mountid, err := daemon.layerStore.GetMountID(c.ID); err == nil {
				daemon.cleanupMountsByID(mountid)
			}
			logrus.Debugf("container stopped %s", c.ID)
		})
	}

	if daemon.accelController != nil {
		daemon.accelController.Stop()
	}

	// trigger libnetwork Stop only if it's initialized
	if daemon.netController != nil {
		daemon.netController.Stop()
	}

	if daemon.layerStore != nil {
		if err := daemon.layerStore.Cleanup(); err != nil {
			logrus.Errorf("Error during layer Store.Cleanup(): %v", err)
		}
	}

	if err := daemon.cleanupMounts(); err != nil {
		return err
	}

	return nil
}

// Mount sets container.BaseFS
// (is it not set coming in? why is it unset?)
func (daemon *Daemon) Mount(container *container.Container) error {
	if container.HostConfig.ExternalRootfs != "" {
		container.BaseFS = container.HostConfig.ExternalRootfs
		return nil
	}
	dir, err := container.RWLayer.Mount(container.GetMountLabel())
	if err != nil {
		return err
	}
	logrus.Debugf("container mounted via layerStore: %v", dir)

	if container.BaseFS != dir {
		// The mount path reported by the graph driver should always be trusted on Windows, since the
		// volume path for a given mounted layer may change over time.  This should only be an error
		// on non-Windows operating systems.
		if container.BaseFS != "" && runtime.GOOS != "windows" {
			daemon.Unmount(container)
			return fmt.Errorf("Error: driver %s is returning inconsistent paths for container %s ('%s' then '%s')",
				daemon.GraphDriverName(), container.ID, container.BaseFS, dir)
		}
	}
	container.BaseFS = dir // TODO: combine these fields
	return nil
}

// Unmount unsets the container base filesystem
func (daemon *Daemon) Unmount(container *container.Container) error {
	if container.HostConfig.ExternalRootfs != "" {
		return nil
	}
	if err := container.RWLayer.Unmount(); err != nil {
		logrus.Errorf("Error unmounting container %s: %s", container.ID, err)
		return err
	}
	return nil
}

// TagImage creates the tag specified by newTag, pointing to the image named
// imageName (alternatively, imageName can also be an image ID).
func (daemon *Daemon) TagImage(newTag reference.Named, imageName string) error {
	imageID, err := daemon.GetImageID(imageName)
	if err != nil {
		return err
	}
	if err := daemon.referenceStore.AddTag(newTag, imageID, true); err != nil {
		return err
	}

	daemon.LogImageEvent(imageID.String(), newTag.String(), "tag")
	return nil
}

func writeDistributionProgress(cancelFunc func(), outStream io.Writer, progressChan <-chan progress.Progress) {
	progressOutput := streamformatter.NewJSONStreamFormatter().NewProgressOutput(outStream, false)
	operationCancelled := false

	for prog := range progressChan {
		if err := progressOutput.WriteProgress(prog); err != nil && !operationCancelled {
			// don't log broken pipe errors as this is the normal case when a client aborts
			if isBrokenPipe(err) {
				logrus.Info("Pull session cancelled")
			} else {
				logrus.Errorf("error writing progress to client: %v", err)
			}
			cancelFunc()
			operationCancelled = true
			// Don't return, because we need to continue draining
			// progressChan until it's closed to avoid a deadlock.
		}
	}
}

func isBrokenPipe(e error) bool {
	if netErr, ok := e.(*net.OpError); ok {
		e = netErr.Err
		if sysErr, ok := netErr.Err.(*os.SyscallError); ok {
			e = sysErr.Err
		}
	}
	return e == syscall.EPIPE
}

// PullImage initiates a pull operation. image is the repository name to pull, and
// tag may be either empty, or indicate a specific tag to pull.
func (daemon *Daemon) PullImage(ctx context.Context, ref reference.Named, metaHeaders map[string][]string, authConfig *types.AuthConfig, outStream io.Writer) error {
	// Include a buffer so that slow client connections don't affect
	// transfer performance.
	progressChan := make(chan progress.Progress, 100)

	writesDone := make(chan struct{})

	ctx, cancelFunc := context.WithCancel(ctx)

	go func() {
		writeDistributionProgress(cancelFunc, outStream, progressChan)
		close(writesDone)
	}()

	imagePullConfig := &distribution.ImagePullConfig{
		MetaHeaders:      metaHeaders,
		AuthConfig:       authConfig,
		ProgressOutput:   progress.ChanOutput(progressChan),
		RegistryService:  daemon.RegistryService,
		ImageEventLogger: daemon.LogImageEvent,
		MetadataStore:    daemon.distributionMetadataStore,
		LayerStore:       daemon.layerStore,
		ImageStore:       daemon.imageStore,
		ReferenceStore:   daemon.referenceStore,
		DownloadManager:  daemon.downloadManager,
	}

	err := distribution.Pull(ctx, ref, imagePullConfig)
	close(progressChan)
	<-writesDone
	return err
}

// PullOnBuild tells Docker to pull image referenced by `name`.
func (daemon *Daemon) PullOnBuild(ctx context.Context, name string, authConfigs map[string]types.AuthConfig, output io.Writer) (builder.Image, error) {
	ref, err := reference.ParseNamed(name)
	if err != nil {
		return nil, err
	}
	ref = reference.WithDefaultTag(ref)

	pullRegistryAuth := &types.AuthConfig{}
	if len(authConfigs) > 0 {
		// The request came with a full auth config file, we prefer to use that
		repoInfo, err := daemon.RegistryService.ResolveRepository(ref)
		if err != nil {
			return nil, err
		}

		resolvedConfig := registry.ResolveAuthConfig(
			authConfigs,
			repoInfo.Index,
		)
		pullRegistryAuth = &resolvedConfig
	}

	if err := daemon.PullImage(ctx, ref, nil, pullRegistryAuth, output); err != nil {
		return nil, err
	}
	return daemon.GetImage(name)
}

// ExportImage exports a list of images to the given output stream. The
// exported images are archived into a tar when written to the output
// stream. All images with the given tag and all versions containing
// the same tag are exported. names is the set of tags to export, and
// outStream is the writer which the images are written to.
func (daemon *Daemon) ExportImage(names []string, outStream io.Writer) error {
	imageExporter := tarexport.NewTarExporter(daemon.imageStore, daemon.layerStore, daemon.referenceStore)
	return imageExporter.Save(names, outStream)
}

// PushImage initiates a push operation on the repository named localName.
func (daemon *Daemon) PushImage(ctx context.Context, ref reference.Named, metaHeaders map[string][]string, authConfig *types.AuthConfig, outStream io.Writer) error {
	// Include a buffer so that slow client connections don't affect
	// transfer performance.
	progressChan := make(chan progress.Progress, 100)

	writesDone := make(chan struct{})

	ctx, cancelFunc := context.WithCancel(ctx)

	go func() {
		writeDistributionProgress(cancelFunc, outStream, progressChan)
		close(writesDone)
	}()

	imagePushConfig := &distribution.ImagePushConfig{
		MetaHeaders:      metaHeaders,
		AuthConfig:       authConfig,
		ProgressOutput:   progress.ChanOutput(progressChan),
		RegistryService:  daemon.RegistryService,
		ImageEventLogger: daemon.LogImageEvent,
		MetadataStore:    daemon.distributionMetadataStore,
		LayerStore:       daemon.layerStore,
		ImageStore:       daemon.imageStore,
		ReferenceStore:   daemon.referenceStore,
		TrustKey:         daemon.trustKey,
		UploadManager:    daemon.uploadManager,
	}

	err := distribution.Push(ctx, ref, imagePushConfig)
	close(progressChan)
	<-writesDone
	return err
}

// LookupImage looks up an image by name and returns it as an ImageInspect
// structure.
func (daemon *Daemon) LookupImage(name string) (*types.ImageInspect, error) {
	img, err := daemon.GetImage(name)
	if err != nil {
		return nil, fmt.Errorf("No such image: %s", name)
	}

	refs := daemon.referenceStore.References(img.ID())
	repoTags := []string{}
	repoDigests := []string{}
	for _, ref := range refs {
		switch ref.(type) {
		case reference.NamedTagged:
			repoTags = append(repoTags, ref.String())
		case reference.Canonical:
			repoDigests = append(repoDigests, ref.String())
		}
	}

	var size int64
	var layerMetadata map[string]string
	layerID := img.RootFS.ChainID()
	if layerID != "" {
		l, err := daemon.layerStore.Get(layerID)
		if err != nil {
			return nil, err
		}
		defer layer.ReleaseAndLog(daemon.layerStore, l)
		size, err = l.Size()
		if err != nil {
			return nil, err
		}

		layerMetadata, err = l.Metadata()
		if err != nil {
			return nil, err
		}
	}

	comment := img.Comment
	if len(comment) == 0 && len(img.History) > 0 {
		comment = img.History[len(img.History)-1].Comment
	}

	imageInspect := &types.ImageInspect{
		ID:              img.ID().String(),
		RepoTags:        repoTags,
		RepoDigests:     repoDigests,
		Parent:          img.Parent.String(),
		From:            img.From,
		Comment:         comment,
		Created:         img.Created.Format(time.RFC3339Nano),
		Container:       img.Container,
		ContainerConfig: &img.ContainerConfig,
		DockerVersion:   img.DockerVersion,
		Author:          img.Author,
		Config:          img.Config,
		Architecture:    img.Architecture,
		Os:              img.OS,
		Size:            size,
		VirtualSize:     size, // TODO: field unused, deprecate
		RootFS:          rootFSToAPIType(img.RootFS),
	}

	imageInspect.GraphDriver.Name = daemon.GraphDriverName()

	imageInspect.GraphDriver.Data = layerMetadata

	return imageInspect, nil
}

// LoadImage uploads a set of images into the repository. This is the
// complement of ImageExport.  The input stream is an uncompressed tar
// ball containing images and metadata.
func (daemon *Daemon) LoadImage(inTar io.ReadCloser, outStream io.Writer, quiet bool, toBePulled bool) error {
	imageExporter := tarexport.NewTarExporter(daemon.imageStore, daemon.layerStore, daemon.referenceStore)
	return imageExporter.Load(inTar, outStream, quiet, toBePulled)
}

// ImageHistory returns a slice of ImageHistory structures for the specified image
// name by walking the image lineage.
func (daemon *Daemon) ImageHistory(name string) ([]*types.ImageHistory, error) {
	img, err := daemon.GetImage(name)
	if err != nil {
		return nil, err
	}

	history := []*types.ImageHistory{}

	layerCounter := 0
	rootFS := *img.RootFS
	rootFS.DiffIDs = nil

	for _, h := range img.History {
		var layerSize int64

		if !h.EmptyLayer {
			if len(img.RootFS.DiffIDs) <= layerCounter {
				return nil, fmt.Errorf("too many non-empty layers in History section")
			}

			rootFS.Append(img.RootFS.DiffIDs[layerCounter])
			l, err := daemon.layerStore.Get(rootFS.ChainID())
			if err != nil {
				return nil, err
			}
			layerSize, err = l.DiffSize()
			layer.ReleaseAndLog(daemon.layerStore, l)
			if err != nil {
				return nil, err
			}

			layerCounter++
		}

		history = append([]*types.ImageHistory{{
			ID:        "<missing>",
			Created:   h.Created.Unix(),
			CreatedBy: h.CreatedBy,
			Comment:   h.Comment,
			Size:      layerSize,
		}}, history...)
	}

	// Fill in image IDs and tags
	histImg := img
	id := img.ID()
	for _, h := range history {
		h.ID = id.String()

		var tags []string
		for _, r := range daemon.referenceStore.References(id) {
			if _, ok := r.(reference.NamedTagged); ok {
				tags = append(tags, r.String())
			}
		}

		h.Tags = tags

		id = histImg.Parent
		if id == "" {
			break
		}
		histImg, err = daemon.GetImage(id.String())
		if err != nil {
			break
		}
	}

	return history, nil
}

// GetImageID returns an image ID corresponding to the image referred to by
// refOrID.
func (daemon *Daemon) GetImageID(refOrID string) (image.ID, error) {
	id, ref, err := reference.ParseIDOrReference(refOrID)
	if err != nil {
		return "", err
	}
	if id != "" {
		if _, err := daemon.imageStore.Get(image.ID(id)); err != nil {
			return "", ErrImageDoesNotExist{refOrID}
		}
		return image.ID(id), nil
	}

	if id, err := daemon.referenceStore.Get(ref); err == nil {
		return id, nil
	}
	if tagged, ok := ref.(reference.NamedTagged); ok {
		if id, err := daemon.imageStore.Search(tagged.Tag()); err == nil {
			for _, namedRef := range daemon.referenceStore.References(id) {
				if namedRef.Name() == ref.Name() {
					return id, nil
				}
			}
		}
	}

	// Search based on ID
	if id, err := daemon.imageStore.Search(refOrID); err == nil {
		return id, nil
	}

	return "", ErrImageDoesNotExist{refOrID}
}

// GetImage returns an image corresponding to the image referred to by refOrID.
func (daemon *Daemon) GetImage(refOrID string) (*image.Image, error) {
	imgID, err := daemon.GetImageID(refOrID)
	if err != nil {
		return nil, err
	}
	return daemon.imageStore.Get(imgID)
}

// GetImageOnBuild looks up a Docker image referenced by `name`.
func (daemon *Daemon) GetImageOnBuild(name string) (builder.Image, error) {
	isComplete, err := daemon.IsCompleteImage(name)
	if err != nil {
		return nil, err
	}

	refOrID := name
	if !isComplete {
		_, refOrID, err = daemon.CreateCompleteImage(name, nil)
		if err != nil {
			return nil, err
		}
	}

	img, err := daemon.GetImage(refOrID)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// GraphDriverName returns the name of the graph driver used by the layer.Store
func (daemon *Daemon) GraphDriverName() string {
	return daemon.layerStore.DriverName()
}

// GetUIDGIDMaps returns the current daemon's user namespace settings
// for the full uid and gid maps which will be applied to containers
// started in this instance.
func (daemon *Daemon) GetUIDGIDMaps() ([]idtools.IDMap, []idtools.IDMap) {
	return daemon.uidMaps, daemon.gidMaps
}

// GetRemappedUIDGID returns the current daemon's uid and gid values
// if user namespaces are in use for this daemon instance.  If not
// this function will return "real" root values of 0, 0.
func (daemon *Daemon) GetRemappedUIDGID() (int, int) {
	uid, gid, _ := idtools.GetRootUIDGID(daemon.uidMaps, daemon.gidMaps)
	return uid, gid
}

func (daemon *Daemon) HoldOnImageByID(id string) error {
	return daemon.imageStore.HoldOn(image.ID(id))
}

func (daemon *Daemon) HoldOffImageByID(id string) error {
	return daemon.imageStore.HoldOff(image.ID(id))
}

// GetCachedImage returns the most recent created image that is a child
// of the image with imgID, that had the same config when it was
// created. nil is returned if a child cannot be found. An error is
// returned if the parent image cannot be found.
func (daemon *Daemon) GetCachedImage(imgID image.ID, config *containertypes.Config) (*image.Image, error) {
	// Loop on the children of the given image and check the config
	getMatch := func(siblings []image.ID) (*image.Image, error) {
		var match *image.Image
		for _, id := range siblings {
			img, err := daemon.imageStore.Get(id)
			if err != nil {
				return nil, fmt.Errorf("unable to find image %q", id)
			}

			// Partial image should create a new complete image before running, so actually
			// partial image can't run by itself. Running a partial image is running the created
			// new complete image, so partial image should not be the cached image.
			if img.From != "" {
				continue
			}

			if runconfig.Compare(&img.ContainerConfig, config) {
				// check for the most up to date match
				if match == nil || match.Created.Before(img.Created) {
					match = img
				}
			}
		}
		return match, nil
	}

	// In this case, this is `FROM scratch`, which isn't an actual image.
	if imgID == "" {
		images := daemon.imageStore.Map()
		var siblings []image.ID
		for id, img := range images {
			if img.Parent == imgID {
				siblings = append(siblings, id)
			}
		}
		return getMatch(siblings)
	}

	// find match from child images
	siblings := daemon.imageStore.Children(imgID)
	return getMatch(siblings)
}

// GetCachedImageOnBuild returns a reference to a cached image whose parent equals `parent`
// and runconfig equals `cfg`. A cache miss is expected to return an empty ID and a nil error.
func (daemon *Daemon) GetCachedImageOnBuild(imgID string, cfg *containertypes.Config) (string, error) {
	cache, err := daemon.GetCachedImage(image.ID(imgID), cfg)
	if cache == nil || err != nil {
		return "", err
	}
	return cache.ID().String(), nil
}

// tempDir returns the default directory to use for temporary files.
func tempDir(rootDir string, rootUID, rootGID int) (string, error) {
	var tmpDir string
	if tmpDir = os.Getenv("DOCKER_TMPDIR"); tmpDir == "" {
		tmpDir = filepath.Join(rootDir, "tmp")
	}
	return tmpDir, idtools.MkdirAllAs(tmpDir, 0700, rootUID, rootGID)
}

func (daemon *Daemon) setSecurityOptions(container *container.Container, hostConfig *containertypes.HostConfig) error {
	container.Lock()
	defer container.Unlock()
	return parseSecurityOpt(container, hostConfig)
}

func (daemon *Daemon) setHostConfig(container *container.Container, hostConfig *containertypes.HostConfig) error {
	// Do not lock while creating volumes since this could be calling out to external plugins
	// Don't want to block other actions, like `docker ps` because we're waiting on an external plugin
	if err := daemon.registerMountPoints(container, hostConfig); err != nil {
		return err
	}

	container.Lock()
	defer container.Unlock()

	// Register any links from the host config before starting the container
	if err := daemon.registerLinks(container, hostConfig); err != nil {
		return err
	}

	// make sure links is not nil
	// this ensures that on the next daemon restart we don't try to migrate from legacy sqlite links
	if hostConfig.Links == nil {
		hostConfig.Links = []string{}
	}

	// register hooks to container
	if err := daemon.registerHooks(container, hostConfig.HookSpec); err != nil {
		return err
	}

	container.HostConfig = hostConfig
	return container.ToDisk()
}

func (daemon *Daemon) setupInitLayer(initPath string) error {
	rootUID, rootGID := daemon.GetRemappedUIDGID()
	return setupInitLayer(initPath, rootUID, rootGID)
}

func setDefaultMtu(config *Config) {
	// do nothing if the config does not have the default 0 value.
	if config.Mtu != 0 {
		return
	}
	config.Mtu = defaultNetworkMtu
}

// verifyContainerSettings performs validation of the hostconfig and config
// structures.
func (daemon *Daemon) verifyContainerSettings(hostConfig *containertypes.HostConfig, config *containertypes.Config, update bool) ([]string, error) {

	// First perform verification of settings common across all platforms.
	if config != nil {
		if config.WorkingDir != "" {
			config.WorkingDir = filepath.FromSlash(config.WorkingDir) // Ensure in platform semantics
			if !system.IsAbs(config.WorkingDir) {
				return nil, fmt.Errorf("The working directory '%s' is invalid. It needs to be an absolute path.", config.WorkingDir)
			}
		}

		if len(config.StopSignal) > 0 {
			_, err := signal.ParseSignal(config.StopSignal)
			if err != nil {
				return nil, err
			}
		}
	}

	if hostConfig == nil {
		return nil, nil
	}

	for port := range hostConfig.PortBindings {
		_, portStr := nat.SplitProtoPort(string(port))
		if _, err := nat.ParsePort(portStr); err != nil {
			return nil, fmt.Errorf("Invalid port specification: %q", portStr)
		}
		for _, pb := range hostConfig.PortBindings[port] {
			_, err := nat.NewPort(nat.SplitProtoPort(pb.HostPort))
			if err != nil {
				return nil, fmt.Errorf("Invalid port specification: %q", pb.HostPort)
			}
		}
	}

	// Now do platform-specific verification
	return verifyPlatformContainerSettings(daemon, hostConfig, config, update)
}

// Checks if the client set configurations for more than one network while creating a container
func (daemon *Daemon) verifyNetworkingConfig(nwConfig *networktypes.NetworkingConfig) error {
	if nwConfig == nil || len(nwConfig.EndpointsConfig) <= 1 {
		return nil
	}
	l := make([]string, 0, len(nwConfig.EndpointsConfig))
	for k := range nwConfig.EndpointsConfig {
		l = append(l, k)
	}
	err := fmt.Errorf("Container cannot be connected to network endpoints: %s", strings.Join(l, ", "))
	return errors.NewBadRequestError(err)
}

func configureVolumes(config *Config, rootUID, rootGID int) (*store.VolumeStore, error) {
	volumesDriver, err := local.New(config.Root, rootUID, rootGID)
	if err != nil {
		return nil, err
	}

	volumedrivers.Register(volumesDriver, volumesDriver.Name())
	return store.New(config.Root)
}

// AuthenticateToRegistry checks the validity of credentials in authConfig
func (daemon *Daemon) AuthenticateToRegistry(ctx context.Context, authConfig *types.AuthConfig) (string, string, error) {
	return daemon.RegistryService.Auth(authConfig, dockerversion.DockerUserAgent(ctx))
}

// SearchRegistryForImages queries the registry for images matching
// term. authConfig is used to login.
func (daemon *Daemon) SearchRegistryForImages(ctx context.Context, term string,
	authConfig *types.AuthConfig,
	headers map[string][]string) (*registrytypes.SearchResults, error) {
	return daemon.RegistryService.Search(term, authConfig, dockerversion.DockerUserAgent(ctx), headers)
}

// IsShuttingDown tells whether the daemon is shutting down or not
func (daemon *Daemon) IsShuttingDown() bool {
	return daemon.shutdown
}

// GetContainerStats collects all the stats published by a container
func (daemon *Daemon) GetContainerStats(container *container.Container) (*types.StatsJSON, error) {
	stats, err := daemon.stats(container)
	if err != nil {
		return nil, err
	}

	if stats.Networks, err = daemon.getNetworkStats(container); err != nil {
		return nil, err
	}

	return stats, nil
}

// Resolve Network SandboxID in case the container reuse another container's network stack
func (daemon *Daemon) getNetworkSandboxID(c *container.Container) (string, error) {
	curr := c
	for curr.HostConfig.NetworkMode.IsContainer() {
		containerID := curr.HostConfig.NetworkMode.ConnectedContainer()
		connected, err := daemon.GetContainer(containerID)
		if err != nil {
			return "", fmt.Errorf("Could not get container for %s", containerID)
		}
		curr = connected
	}
	return curr.NetworkSettings.SandboxID, nil
}

func (daemon *Daemon) getNetworkStats(c *container.Container) (map[string]types.NetworkStats, error) {
	sandboxID, err := daemon.getNetworkSandboxID(c)
	if err != nil {
		return nil, err
	}

	sb, err := daemon.netController.SandboxByID(sandboxID)
	if err != nil {
		return nil, err
	}

	lnstats, err := sb.Statistics()
	if err != nil {
		return nil, err
	}

	stats := make(map[string]types.NetworkStats)
	// Convert libnetwork nw stats into engine-api stats
	for ifName, ifStats := range lnstats {
		stats[ifName] = types.NetworkStats{
			RxBytes:   ifStats.RxBytes,
			RxPackets: ifStats.RxPackets,
			RxErrors:  ifStats.RxErrors,
			RxDropped: ifStats.RxDropped,
			TxBytes:   ifStats.TxBytes,
			TxPackets: ifStats.TxPackets,
			TxErrors:  ifStats.TxErrors,
			TxDropped: ifStats.TxDropped,
		}
	}

	return stats, nil
}

// newBaseContainer creates a new container with its initial
// configuration based on the root storage from the daemon.
func (daemon *Daemon) newBaseContainer(id string) *container.Container {
	return container.NewBaseContainer(id, daemon.containerRoot(id))
}

// initDiscovery initializes the discovery watcher for this daemon.
func (daemon *Daemon) initDiscovery(config *Config) error {
	advertise, err := parseClusterAdvertiseSettings(config.ClusterStore, config.ClusterAdvertise)
	if err != nil {
		if err == errDiscoveryDisabled {
			return nil
		}
		return err
	}

	config.ClusterAdvertise = advertise
	discoveryWatcher, err := initDiscovery(config.ClusterStore, config.ClusterAdvertise, config.ClusterOpts)
	if err != nil {
		return fmt.Errorf("discovery initialization failed (%v)", err)
	}

	daemon.discoveryWatcher = discoveryWatcher
	return nil
}

// Reload reads configuration changes and modifies the
// daemon according to those changes.
// This are the settings that Reload changes:
// - Daemon labels.
// - Cluster discovery (reconfigure and restart).
// - Daemon live restore
func (daemon *Daemon) Reload(config *Config) error {
	daemon.configStore.reloadLock.Lock()
	defer daemon.configStore.reloadLock.Unlock()
	if config.IsValueSet("labels") {
		daemon.configStore.Labels = config.Labels
	}
	if config.IsValueSet("debug") {
		daemon.configStore.Debug = config.Debug
	}
	if config.IsValueSet("live-restore") {
		daemon.configStore.LiveRestore = config.LiveRestore
		if err := daemon.containerdRemote.UpdateOptions(libcontainerd.WithLiveRestore(config.LiveRestore)); err != nil {
			return err
		}

	}
	return daemon.reloadClusterDiscovery(config)
}

func (daemon *Daemon) reloadClusterDiscovery(config *Config) error {
	var err error
	newAdvertise := daemon.configStore.ClusterAdvertise
	newClusterStore := daemon.configStore.ClusterStore
	if config.IsValueSet("cluster-advertise") {
		if config.IsValueSet("cluster-store") {
			newClusterStore = config.ClusterStore
		}
		newAdvertise, err = parseClusterAdvertiseSettings(newClusterStore, config.ClusterAdvertise)
		if err != nil && err != errDiscoveryDisabled {
			return err
		}
	}

	// check discovery modifications
	if !modifiedDiscoverySettings(daemon.configStore, newAdvertise, newClusterStore, config.ClusterOpts) {
		return nil
	}

	// enable discovery for the first time if it was not previously enabled
	if daemon.discoveryWatcher == nil {
		discoveryWatcher, err := initDiscovery(newClusterStore, newAdvertise, config.ClusterOpts)
		if err != nil {
			return fmt.Errorf("discovery initialization failed (%v)", err)
		}
		daemon.discoveryWatcher = discoveryWatcher
	} else {
		if err == errDiscoveryDisabled {
			// disable discovery if it was previously enabled and it's disabled now
			daemon.discoveryWatcher.Stop()
		} else {
			// reload discovery
			if err = daemon.discoveryWatcher.Reload(config.ClusterStore, newAdvertise, config.ClusterOpts); err != nil {
				return err
			}
		}
	}

	daemon.configStore.ClusterStore = newClusterStore
	daemon.configStore.ClusterOpts = config.ClusterOpts
	daemon.configStore.ClusterAdvertise = newAdvertise

	if daemon.netController == nil {
		return nil
	}
	netOptions, err := daemon.networkOptions(daemon.configStore, nil)
	if err != nil {
		logrus.Warnf("Failed to reload configuration with network controller: %v", err)
		return nil
	}
	err = daemon.netController.ReloadConfiguration(netOptions...)
	if err != nil {
		logrus.Warnf("Failed to reload configuration with network controller: %v", err)
	}

	return nil
}

func validateID(id string) error {
	if id == "" {
		return fmt.Errorf("Invalid empty id")
	}
	return nil
}

func isBridgeNetworkDisabled(config *Config) bool {
	return config.bridgeConfig.Iface == disableNetworkBridge
}

func (daemon *Daemon) networkOptions(dconfig *Config, activeSandboxes map[string]interface{}) ([]nwconfig.Option, error) {
	options := []nwconfig.Option{}
	if dconfig == nil {
		return options, nil
	}

	options = append(options, nwconfig.OptionDataDir(dconfig.Root))

	dd := runconfig.DefaultDaemonNetworkMode()
	dn := runconfig.DefaultDaemonNetworkMode().NetworkName()
	options = append(options, nwconfig.OptionDefaultDriver(string(dd)))
	options = append(options, nwconfig.OptionDefaultNetwork(dn))

	if strings.TrimSpace(dconfig.ClusterStore) != "" {
		kv := strings.Split(dconfig.ClusterStore, "://")
		if len(kv) != 2 {
			return nil, fmt.Errorf("kv store daemon config must be of the form KV-PROVIDER://KV-URL")
		}
		options = append(options, nwconfig.OptionKVProvider(kv[0]))
		options = append(options, nwconfig.OptionKVProviderURL(kv[1]))
	}
	if len(dconfig.ClusterOpts) > 0 {
		options = append(options, nwconfig.OptionKVOpts(dconfig.ClusterOpts))
	}

	if daemon.discoveryWatcher != nil {
		options = append(options, nwconfig.OptionDiscoveryWatcher(daemon.discoveryWatcher))
	}

	if dconfig.ClusterAdvertise != "" {
		options = append(options, nwconfig.OptionDiscoveryAddress(dconfig.ClusterAdvertise))
	}

	options = append(options, nwconfig.OptionLabels(dconfig.Labels))
	options = append(options, driverOptions(dconfig)...)
	if daemon.configStore != nil && len(activeSandboxes) != 0 {
		options = append(options, nwconfig.OptionActiveSandboxes(activeSandboxes))
	}
	return options, nil
}

func copyBlkioEntry(entries []*containerd.BlkioStatsEntry) []types.BlkioStatEntry {
	out := make([]types.BlkioStatEntry, len(entries))
	for i, re := range entries {
		out[i] = types.BlkioStatEntry{
			Major: re.Major,
			Minor: re.Minor,
			Op:    re.Op,
			Value: re.Value,
		}
	}
	return out
}

func (daemon *Daemon) sanitizeHookSpec(spec string) (string, error) {
	if spec != "" {
		spec = filepath.Clean(spec)
		if !filepath.IsAbs(spec) {
			return "", fmt.Errorf("hook spec file must be an absolute path")
		}
		fi, err := os.Stat(spec)
		if err != nil {
			return "", fmt.Errorf("stat hook spec file failed: %v", err)
		}
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("hook spec file must be a regular text file")
		}
	}
	return spec, nil
}

func (daemon *Daemon) initHooks(config *Config, rootUID, rootGID int) error {
	// create hook store dir
	var err error
	hookDir := filepath.Join(config.Root, "hooks")
	if err = idtools.MkdirAllAs(hookDir, 0700, rootUID, rootGID); err != nil && !os.IsExist(err) {
		return err
	}
	daemon.hookStore = hookDir

	if config.HookSpec, err = daemon.sanitizeHookSpec(config.HookSpec); err != nil {
		return err
	}

	//setup default hooks
	if err := daemon.registerDaemonHooks(config.HookSpec); err != nil {
		return err
	}

	return nil
}

func (daemon *Daemon) registerDaemonHooks(hookspec string) error {
	if hookspec == "" {
		return nil
	}

	// the hook spec has already been sanitized, so no need for validation again
	f, err := os.Open(hookspec)
	if err != nil {
		return fmt.Errorf("open hook spec file error: %v", err)
	}
	defer f.Close()

	if err = json.NewDecoder(f).Decode(&daemon.Hooks); err != nil {
		return fmt.Errorf("malformed hook spec, is your spec file in json format? error: %v", err)
	}

	// hook path must be absolute and must be subdir of XXX
	if err = daemon.validateHook(&daemon.Hooks); err != nil {
		return err
	}

	return nil
}

func (daemon *Daemon) registerHooks(container *container.Container, hookspec string) error {
	container.Hooks.Prestart = append(container.Hooks.Prestart, daemon.Hooks.Prestart...)
	container.Hooks.Poststart = append(container.Hooks.Poststart, daemon.Hooks.Poststart...)
	container.Hooks.Poststop = append(container.Hooks.Poststop, daemon.Hooks.Poststop...)
	if hookspec == "" {
		return nil
	}

	// the hook spec has already been sanitized, so no need for validation again
	f, err := os.Open(hookspec)
	if err != nil {
		return fmt.Errorf("open hook spec file error: %v", err)
	}
	defer f.Close()

	var hooks specs.Hooks
	if err = json.NewDecoder(f).Decode(&hooks); err != nil {
		return fmt.Errorf("malformed hook spec, is your spec file in json format? error: %v", err)
	}

	container.Hooks.Prestart = append(container.Hooks.Prestart, hooks.Prestart...)
	container.Hooks.Poststart = append(container.Hooks.Poststart, hooks.Poststart...)
	container.Hooks.Poststop = append(container.Hooks.Poststop, hooks.Poststop...)

	// hook path must be absolute and must be subdir of XXX
	if err = daemon.validateHook(&container.Hooks); err != nil {
		return err
	}

	return nil
}

func (daemon *Daemon) validateHook(hooks *specs.Hooks) error {
	for _, v := range hooks.Prestart {
		if err := daemon.validateHookPath(v.Path); err != nil {
			return err
		}
	}
	for _, v := range hooks.Poststart {
		if err := daemon.validateHookPath(v.Path); err != nil {
			return err
		}
	}
	for _, v := range hooks.Poststop {
		if err := daemon.validateHookPath(v.Path); err != nil {
			return err
		}
	}
	return nil
}

func (daemon *Daemon) validateHookPath(path string) error {
	// hook path must be absolute and must be subdir of XXX
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("Hook path %q must be an absolute path", path)
	}

	if !filepath.HasPrefix(path, daemon.hookStore) {
		return fmt.Errorf("hook path %q isn't right, hook program must be put under %q", path, daemon.hookStore)
	}

	return nil
}
