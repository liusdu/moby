package reference

import (
	"sync"
)

var (
	rSR = &referenceStateMgr{
		repoTagMap: make(map[string]refCount),
	}
)

type refCount int

func (ref refCount) increase() refCount {
	return refCount(int(ref) + 1)
}

func (ref refCount) decrease() refCount {
	return refCount(int(ref) - 1)
}

func (ref refCount) shouldRelease() bool {
	return int(ref) <= 0
}

type referenceStateMgr struct {
	sync.Mutex
	repoTagMap map[string]refCount
}

func (rsm *referenceStateMgr) updateTagRefcount(tag string, inc bool) {
	rsm.Lock()
	defer rsm.Unlock()

	if !inc {
		rsm.repoTagMap[tag] = rsm.repoTagMap[tag].decrease()
		if rsm.repoTagMap[tag].shouldRelease() {
			delete(rsm.repoTagMap, tag)
		}
		return
	}

	rsm.repoTagMap[tag] = rsm.repoTagMap[tag].increase()
}

func (rsm *referenceStateMgr) isRepoTagHandlingInProcess(tag string) bool {
	rsm.Lock()
	defer rsm.Unlock()
	if _, exist := rsm.repoTagMap[tag]; !exist {
		return false
	}

	return true
}

// TryUpdateRepoTagHandlingStatus updates the repo tag status in referenceStateMgr
func (rsm *referenceStateMgr) tryUpdateRepoTagHandlingStatus(tag string, handle bool) bool {
	rsm.Lock()
	defer rsm.Unlock()
	if _, exist := rsm.repoTagMap[tag]; exist {
		return false
	}

	if !handle {
		rsm.repoTagMap[tag] = rsm.repoTagMap[tag].decrease()
		if rsm.repoTagMap[tag].shouldRelease() {
			delete(rsm.repoTagMap, tag)
		}
		return true
	}

	rsm.repoTagMap[tag] = rsm.repoTagMap[tag].increase()
	return true
}

// UpdateRepoTagHandlingStatus updates the repo tag status in referenceStateMgr
func UpdateRepoTagHandlingStatus(tag string, handle bool) {
	rSR.updateTagRefcount(tag, handle)
}

// TryUpdateRepoTagHandlingStatus updates the repo tag status in referenceStateMgr
func TryUpdateRepoTagHandlingStatus(tag string, handle bool) bool {
	return rSR.tryUpdateRepoTagHandlingStatus(tag, handle)
}

// IsRepoTagHandlingInProcess returns if a repo tag is loading in progress
func IsRepoTagHandlingInProcess(tag string) bool {
	return rSR.isRepoTagHandlingInProcess(tag)
}
