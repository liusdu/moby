package image

import (
	"sync"
)

var (
	iSR = &imageStateMgr{
		imgIDMap: make(map[ID]refCount),
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

type imageStateMgr struct {
	sync.Mutex
	imgIDMap map[ID]refCount
}

func (ism *imageStateMgr) updateIDRefcount(id ID, inc bool) {
	ism.Lock()
	defer ism.Unlock()

	if !inc {
		ism.imgIDMap[id] = ism.imgIDMap[id].decrease()
		if ism.imgIDMap[id].shouldRelease() {
			delete(ism.imgIDMap, id)
		}
		return
	}

	ism.imgIDMap[id] = ism.imgIDMap[id].increase()
}

func (ism *imageStateMgr) isImageIDInSavingInProcess(id ID) bool {
	ism.Lock()
	defer ism.Unlock()
	if _, exist := ism.imgIDMap[id]; !exist {
		return false
	}

	return true
}

// UpdateImageIDSavingStatus updates the image saving status in imageStateMgr
func UpdateImageIDSavingStatus(id ID, startSavingImage bool) {
	iSR.updateIDRefcount(id, startSavingImage)
}

// IsImageInSavingInProcess returns if an image is in saving process by image ID
func IsImageInSavingInProcess(id ID) bool {
	return iSR.isImageIDInSavingInProcess(id)
}
