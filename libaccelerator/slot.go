package libaccelerator

import (
	"encoding/json"
	"fmt"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/libaccelerator/datastore"
	"github.com/docker/docker/libaccelerator/driverapi"
)

const (
	// GlobalScope indicates global visible accelerator slot
	GlobalScope = "global"
	// LocalScope indicates accelerator slot only visible in container
	ContainerScope = "container"
)

// Slot defines the interface type a slot need to meet, include some check an manipulate handler
type Slot interface {
	// Name returns the name of the slot
	Name() string
	// ID returns the ID of the slot
	ID() string
	// Runtime returns the runtime of the slot
	Runtime() string
	// DriverName returns the driver name of the slot
	DriverName() string
	// Device returns the device used by the slot
	Device() string
	// Options returns the options used to create the slot
	Options() []string
	// Scope returns the scope of the slot
	Scope() string
	// Owner returns the owner of the slot
	Owner() string
	// SetOwner is used to set an new owner
	SetOwner(owner string)
	// State returns the state of the slot
	State() int
	// IsBadDriver check whether the slot is in BadDriver state
	IsBadDriver() bool
	// IsNoDev check whether the slot is in NoDev state
	IsNoDev() bool

	// Prepare is used to prepare the slot environment used by the container
	Prepare() ([]driverapi.Mount, []string, map[string]string, error)
	// Release is used to release the slot
	Release() error
}

type slot struct {
	ctrlr *controller
	sync.Mutex
	// persistent fields
	name       string
	id         string
	scope      string
	driverName string
	runtime    string
	options    []string
	owner      string
	state      int
	// in-memory fields
	dbIndex  uint64
	dbExists bool
}

const SLOT_STATE_BADDRIVER = 0x1
const SLOT_STATE_INDELETE = 0x2
const SLOT_STATE_NODEV = 0x4

// Name returns the name of the slot
func (s *slot) Name() string {
	s.Lock()
	defer s.Unlock()
	return s.name
}

// ID returns the ID of the slot
func (s *slot) ID() string {
	s.Lock()
	defer s.Unlock()
	return s.id
}

// Runtime returns the runtime of the slot
func (s *slot) Runtime() string {
	s.Lock()
	defer s.Unlock()
	return s.runtime
}

// Options returns the options used to create the slot
func (s *slot) Options() []string {
	s.Lock()
	defer s.Unlock()
	return s.options
}

// DriverName returns the driver name of the slot
func (s *slot) DriverName() string {
	s.Lock()
	defer s.Unlock()
	return s.driverName
}

// Device returns the device used by the slot
func (s *slot) Device() string {
	d, err := s.driver(true)
	if err != nil {
		log.Errorf("Failed to connect to accelerator driver plugin: %v", err)
		return ""
	}
	info, err := d.Slot(s.id)
	if err != nil {
		log.Errorf("Failed to get slot info: %v", err)
		return ""
	}
	return info.Device
}

// Scope returns the scope of the slot
func (s *slot) Scope() string {
	s.Lock()
	defer s.Unlock()
	return s.scope
}

// Owner returns the owner of the slot
func (s *slot) Owner() string {
	s.Lock()
	defer s.Unlock()
	return s.owner
}

// SetOwner is used to set an new owner
func (s *slot) SetOwner(owner string) {
	s.Lock()
	s.owner = owner
	s.Unlock()
	if err := s.ctrlr.updateToStore(s); err != nil {
		log.Errorf("error set owner to slot %s: %v", s.id, err)
	}
}

// State returns the state of the slot
func (s *slot) State() int {
	s.Lock()
	defer s.Unlock()
	return s.state
}

// Prepare is used to prepare the slot environment used by the container
func (s *slot) Prepare() ([]driverapi.Mount, []string, map[string]string, error) {
	d, err := s.driver(true)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load accelerator driver: %v", err)
	}

	slotCfg, err := d.PrepareSlot(s.id)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to prepare accelerator slot: %v", err)
	}

	return slotCfg.Binds, slotCfg.Devices, slotCfg.Envs, nil
}

// Release is used to release the slot
func (s *slot) Release() error {
	if s.scope == GlobalScope && s.owner != "" {
		log.Debugf("Release GlobalScope slot %s with non null owner %s", s.id, s.owner)
	}
	return s.release(true)
}

func (s *slot) release(force bool) error {
	s.Lock()
	c := s.ctrlr
	id := s.id
	s.Unlock()

	// Mark the slot for deletion
	if err := s.markInDelete(); err != nil {
		return fmt.Errorf("error marking slot %s for deletion: %v", id, err)
	}

	d, err := s.driver(true)
	if err != nil {
		if !force {
			return fmt.Errorf("failed to load accelerator driver: %v", err)
		}
		log.Debugf("failed to load driver for slot %s: %v", id, err)
	} else if err := d.ReleaseSlot(id); err != nil {
		if !force {
			return fmt.Errorf("failed to release accelerator slot: %v", err)
		}
		log.Debugf("driver failed to delete stale slot %s: %v", id, err)
	}

	if err = c.deleteFromStore(s); err != nil {
		return fmt.Errorf("error deleting slot from store: %v", err)
	}

	return nil
}

/* datastore.KVObject Interface */
func (s *slot) Key() []string {
	s.Lock()
	defer s.Unlock()
	return []string{datastore.SlotKeyPrefix, s.id}
}

func (s *slot) KeyPrefix() []string {
	return []string{datastore.SlotKeyPrefix}
}

func (s *slot) Value() []byte {
	s.Lock()
	defer s.Unlock()
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return b
}

func (s *slot) SetValue(value []byte) error {
	return json.Unmarshal(value, s)
}

func (s *slot) Index() uint64 {
	s.Lock()
	defer s.Unlock()
	return s.dbIndex
}

func (s *slot) SetIndex(index uint64) {
	s.Lock()
	s.dbIndex = index
	s.dbExists = true
	s.Unlock()
}

func (s *slot) Exists() bool {
	s.Lock()
	defer s.Unlock()
	return s.dbExists
}

func (s *slot) DataScope() string {
	return s.Scope()
}

func (s *slot) Skip() bool { return false }

/* Begin datastore.KVConstructor interface */
func (s *slot) New() datastore.KVObject {
	s.Lock()
	defer s.Unlock()

	return &slot{
		ctrlr: s.ctrlr,
		scope: s.scope,
	}
}

func (s *slot) CopyTo(o datastore.KVObject) error {
	s.Lock()
	defer s.Unlock()

	dstS := o.(*slot)
	dstS.name = s.name
	dstS.id = s.id
	dstS.scope = s.scope
	dstS.driverName = s.driverName
	dstS.runtime = s.runtime
	dstS.options = s.options
	dstS.owner = s.owner
	dstS.state = s.state
	dstS.dbIndex = s.dbIndex
	dstS.dbExists = s.dbExists

	return nil
}

// MarshalJSON is used to encode slot into json format
func (s *slot) MarshalJSON() ([]byte, error) {
	slotMap := make(map[string]interface{})

	slotMap["name"] = s.name
	slotMap["id"] = s.id
	slotMap["scope"] = s.scope
	slotMap["driverName"] = s.driverName
	slotMap["runtime"] = s.runtime
	slotMap["options"] = s.options
	slotMap["owner"] = s.owner
	slotMap["state"] = s.state

	return json.Marshal(slotMap)
}

// UnmarshalJSON is used to decode json into slot
func (s *slot) UnmarshalJSON(b []byte) error {
	var slotMap map[string]interface{}
	if err := json.Unmarshal(b, &slotMap); err != nil {
		return err
	}

	s.name = slotMap["name"].(string)
	s.id = slotMap["id"].(string)
	s.scope = slotMap["scope"].(string)
	s.driverName = slotMap["driverName"].(string)
	s.runtime = slotMap["runtime"].(string)
	s.owner = slotMap["owner"].(string)

	// back-compatible
	if options, ok := slotMap["options"].([]interface{}); ok {
		s.options = make([]string, len(options))
		for idx, opt := range options {
			s.options[idx] = opt.(string)
		}
	} else {
		s.options = make([]string, 0)
	}
	if state, ok := slotMap["state"].(float64); ok {
		s.state = int(state)
	} else {
		s.state = 0
	}

	return nil
}

func (s *slot) driver(load bool) (driverapi.Driver, error) {
	d, _, err := s.resolveDriver(s.driverName, load)
	return d, err
}

func (s *slot) resolveDriver(name string, load bool) (driverapi.Driver, *driverapi.Capability, error) {
	c := s.getController()

	// Check if a driver for the specified network type is available
	d, cap := c.drvRegistry.Driver(name)
	if d == nil {
		if load {
			var err error
			err = c.loadDriver(name)
			if err != nil {
				return nil, nil, err
			}

			d, cap = c.drvRegistry.Driver(name)
			if d == nil {
				return nil, nil, fmt.Errorf("could not resolve driver %s in registry", name)
			}
		} else {
			// don't fail if driver loading is not required
			return nil, nil, nil
		}
	}

	return d, cap, nil
}

func (s *slot) getController() *controller {
	s.Lock()
	defer s.Unlock()
	return s.ctrlr
}

func (s *slot) isInDelete() bool {
	s.Lock()
	defer s.Unlock()
	return s.state&SLOT_STATE_INDELETE != 0
}

func (s *slot) markInDelete() error {
	if s.isInDelete() {
		return nil
	}
	s.Lock()
	s.state = s.state | SLOT_STATE_INDELETE
	s.Unlock()
	if err := s.ctrlr.updateToStore(s); err != nil {
		return err
	}
	return nil
}

// IsBadDriver check whether the slot is in BadDriver state
func (s *slot) IsBadDriver() bool {
	s.Lock()
	defer s.Unlock()
	return s.state&SLOT_STATE_BADDRIVER != 0
}
func (s *slot) markBadDriver() error {
	if s.IsBadDriver() {
		return nil
	}
	s.Lock()
	s.state = s.state | SLOT_STATE_BADDRIVER
	s.Unlock()
	if err := s.ctrlr.updateToStore(s); err != nil {
		return err
	}
	return nil
}
func (s *slot) unmarkBadDriver() error {
	if !s.IsBadDriver() {
		return nil
	}
	s.Lock()
	s.state = s.state &^ SLOT_STATE_BADDRIVER
	s.Unlock()
	if err := s.ctrlr.updateToStore(s); err != nil {
		return err
	}
	return nil
}

// IsNoDev check whether the slot is in NoDev state
func (s *slot) IsNoDev() bool {
	s.Lock()
	defer s.Unlock()
	return s.state&SLOT_STATE_NODEV != 0
}

func (s *slot) markNoDev() error {
	if s.IsNoDev() {
		return nil
	}
	s.Lock()
	s.state = s.state | SLOT_STATE_NODEV
	s.Unlock()
	if err := s.ctrlr.updateToStore(s); err != nil {
		return err
	}
	return nil
}

func (s *slot) unmarkNoDev() error {
	if !s.IsNoDev() {
		return nil
	}
	s.Lock()
	s.state = s.state &^ SLOT_STATE_NODEV
	s.Unlock()
	if err := s.ctrlr.updateToStore(s); err != nil {
		return err
	}
	return nil
}
