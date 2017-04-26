package libaccelerator

import (
	"fmt"
	"os"
	"path/filepath"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/libaccelerator/datastore"
	"github.com/docker/docker/libaccelerator/types"
	"github.com/docker/docker/pkg/stringid"
	"github.com/docker/libkv/store/boltdb"
)

const (
	defaultRootPath = "/var/lib/docker"
	accelDataDir    = "accelerator"
	dbName          = "accel.db"
)

func (c *controller) initStore(rootPath string) error {
	boltdb.Register()

	if rootPath == "" {
		rootPath = defaultRootPath
	}
	accPath := filepath.Join(rootPath, accelDataDir)
	if err := os.MkdirAll(accPath, 750); err != nil {
		return err
	}
	dbPath := filepath.Join(accPath, dbName)

	for _, scope := range []string{GlobalScope, ContainerScope} {
		if store, err := datastore.NewDataStore(dbPath, scope); err != nil {
			return err
		} else {
			c.Lock()
			c.stores = append(c.stores, store)
			c.Unlock()
		}
	}

	return nil
}

func (c *controller) closeStores() {
	for _, store := range c.getStores() {
		store.Close()
	}
}

func (c *controller) getStore(scope string) datastore.DataStore {
	c.Lock()
	defer c.Unlock()

	for _, store := range c.stores {
		if store.Scope() == scope {
			return store
		}
	}

	return nil
}

func (c *controller) getStores() []datastore.DataStore {
	c.Lock()
	defer c.Unlock()

	return c.stores
}

func (c *controller) getSlot(sid string) (*slot, error) {
	for _, store := range c.getStores() {
		s := &slot{id: sid, ctrlr: c}
		err := store.GetObject(datastore.Key(s.Key()...), s)
		if err != nil {
			if err != datastore.ErrKeyNotFound {
				log.Debugf("could not find slot %s: %v", sid, err)
			}
			continue
		}
		s.scope = store.Scope()
		return s, nil
	}

	return nil, fmt.Errorf("slot %s not found", sid)
}

func (c *controller) getSlotsForScope(scope string) ([]*slot, error) {
	var sl []*slot

	store := c.getStore(scope)
	if store == nil {
		return nil, fmt.Errorf("scope %s not found", scope)
	}

	kvol, err := store.List(datastore.Key(datastore.SlotKeyPrefix),
		&slot{ctrlr: c})
	if err != nil && err != datastore.ErrKeyNotFound {
		return nil, fmt.Errorf("failed to get slots for scope %s: %v",
			scope, err)
	}

	for _, kvo := range kvol {
		s := kvo.(*slot)
		s.Lock()
		s.ctrlr = c
		s.scope = scope
		s.Unlock()
		sl = append(sl, s)
	}

	return sl, nil
}

func (c *controller) getSlots() ([]*slot, error) {
	var sl []*slot

	for _, store := range c.getStores() {
		kvol, err := store.List(datastore.Key(datastore.SlotKeyPrefix),
			&slot{ctrlr: c})
		if err != nil {
			if err != datastore.ErrKeyNotFound {
				log.Debugf("failed to get slots for scope %s: %v", store.Scope(), err)
			}
			continue
		}

		for _, kvo := range kvol {
			s := kvo.(*slot)
			s.Lock()
			s.ctrlr = c
			s.scope = store.Scope()
			s.Unlock()
			sl = append(sl, s)
		}
	}

	return sl, nil
}

func (c *controller) updateToStore(kvObject datastore.KVObject) error {
	cs := c.getStore(kvObject.DataScope())
	if cs == nil {
		return fmt.Errorf("datastore for scope %q is not initialized", kvObject.DataScope())
	}

	if err := cs.PutObjectAtomic(kvObject); err != nil {
		if err == datastore.ErrKeyModified {
			return err
		}
		return fmt.Errorf("failed to update store for object type %T: %v", kvObject, err)
	}

	return nil
}

func (c *controller) deleteFromStore(kvObject datastore.KVObject) error {
	cs := c.getStore(kvObject.DataScope())
	if cs == nil {
		return fmt.Errorf("datastore for scope %q is not initialized", kvObject.DataScope())
	}

retry:
	if err := cs.DeleteObjectAtomic(kvObject); err != nil {
		if err == datastore.ErrKeyModified {
			if err := cs.GetObject(datastore.Key(kvObject.Key()...), kvObject); err != nil {
				return fmt.Errorf("could not update the kvobject to latest when trying to delete: %v", err)
			}
			goto retry
		}
		return err
	}

	return nil
}

func (c *controller) CleanupSlots(cleaner SlotWalker) {
	slots, err := c.getSlots()
	if err != nil {
		log.Warnf("Could not retrieve slots from stores druing slot cleanup: %v", err)
		return
	}

	// slot cleanup rules
	//  - never touch slot without driver, only mark it as BADDRIVER
	//  - only cleanup IN-DELETE slot

	log.Debugf("libacc: cleanup accelerator slots")
	badDrivers := make(map[string]bool)
	for _, s := range slots {
		// slot in bad driver list
		if badDrivers[s.driverName] {
			s.markBadDriver()
			continue
		}
		// or it has a bad driver
		d, _ := s.driver(false)
		if d == nil {
			badDrivers[s.driverName] = true
			s.markBadDriver()
			continue
		}
		// clear BADDRIVER
		s.unmarkBadDriver()
		// clear NODEV
		s.unmarkNoDev()

		// if driver report the slot is not exist, mark it as NODEV
		if _, err := d.Slot(s.id); err != nil {
			if _, isNotFound := err.(types.NotFoundError); isNotFound {
				// try to recover non-exist slot by re-allocate it
				if _, err := c.allocateSlot(s); err != nil {
					s.markNoDev()
					log.Debugf(" ... found non-exist slot %s, runtime %s@%s, recover failed: %v",
						stringid.TruncateID(s.ID()), s.Runtime(),
						s.DriverName(), err)
				} else {
					s.unmarkNoDev()
					log.Debugf(" ... recover from non-exist slot %s, runtime %s@%s",
						stringid.TruncateID(s.ID()), s.Runtime(),
						s.DriverName())
				}
			} else {
				s.markBadDriver()
				// do not add this driver to []badDriver list
				// only this slot is bad, the driver is ok
				log.Debugf(" ... found bad driver slot %s, runtime %s@%s",
					stringid.TruncateID(s.ID()), s.Runtime(),
					s.DriverName())
			}
		}
	}

	if cleaner != nil {
		c.WalkSlots(cleaner)
	}

	// show accelerator summary
	validCnt := 0
	invalidCnt := 0
	log.Infof("libacc: Accelerator slots summary")
	c.WalkSlots(func(s Slot) bool {
		var state string
		if s.State() == 0 {
			validCnt = validCnt + 1
			if s.Owner() == "" {
				state = "FREE"
			} else {
				state = "USED"
			}
		} else {
			invalidCnt = invalidCnt + 1
			if s.IsBadDriver() {
				state = "BADDRV"
			} else if s.IsNoDev() {
				state = "NODEV"
			} else {
				state = "ERR"
			}
		}
		log.Infof("  ... %s: name %s, runtime %s@%s, state %s",
			stringid.TruncateID(s.ID()), s.Name(),
			s.Runtime(), s.DriverName(), state)
		return false
	})
	log.Infof("libacc: %d valid slots, %d invalid slots", validCnt, invalidCnt)
}
