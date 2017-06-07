package image

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution/digest"
	"github.com/docker/docker/layer"
)

var (
	ErrReferenceCountNotZero = errors.New("can not remove image: reference is not zero")
)

// Store is an interface for creating and accessing images
type Store interface {
	Create(config []byte) (ID, error)
	Get(id ID) (*Image, error)
	Delete(id ID) ([]layer.Metadata, error)
	HoldOn(id ID) error
	HoldOff(id ID) error
	Search(partialID string) (ID, error)
	SetParent(id ID, parent ID) error
	GetParent(id ID) (ID, error)
	Children(id ID) []ID
	Map() map[ID]*Image
	Heads() map[ID]*Image
	GenCicKey(ids []ID) string
	FindCicID(key string) ID
	AddCicMapping(key string, id ID)
}

// LayerGetReleaser is a minimal interface for getting and releasing images.
type LayerGetReleaser interface {
	Get(layer.ChainID) (layer.Layer, error)
	Release(layer.Layer) ([]layer.Metadata, error)
}

type imageMeta struct {
	layer          layer.Layer
	children       map[ID]struct{}
	referenceCount int
}

type store struct {
	sync.Mutex
	ls        LayerGetReleaser
	images    map[ID]*imageMeta
	fs        StoreBackend
	digestSet *digest.Set
	// cic means 'Combined Image CacheID'. key is ids of child images
	// joined by '-', like db27b1f78d42-537c3dac5c5b-e498a0b91c2b
	// value is ID of combined image cached.
	cic map[string]ID
}

// NewImageStore returns new store object for given layer store
func NewImageStore(fs StoreBackend, ls LayerGetReleaser) (Store, error) {
	is := &store{
		ls:        ls,
		images:    make(map[ID]*imageMeta),
		fs:        fs,
		digestSet: digest.NewSet(),
		cic:       make(map[string]ID),
	}

	// load all current images and retain layers
	if err := is.restore(); err != nil {
		return nil, err
	}

	return is, nil
}

func (is *store) restore() error {
	err := is.fs.Walk(func(id ID) error {
		img, err := is.Get(id)
		if err != nil {
			logrus.Errorf("invalid image %v, %v", id, err)
			return nil
		}
		var l layer.Layer
		if chainID := img.RootFS.ChainID(); chainID != "" {
			l, err = is.ls.Get(chainID)
			if err != nil {
				logrus.Errorf("Failed to restore layer %s for image %s: %v", chainID, id, err)
				// If the layer doesn't exist, that means something wrong with layers of the image.
				// 'Layer doesn't exist' means all the images which contain this layer couldn't run successfully.
				// But docker need to continue to work, so here ignore this image, and return nil.
				if err == layer.ErrLayerDoesNotExist {
					return nil
				}
				return err
			}
		}
		if err := is.digestSet.Add(digest.Digest(id)); err != nil {
			return err
		}

		imageMeta := &imageMeta{
			layer:          l,
			children:       make(map[ID]struct{}),
			referenceCount: 0,
		}

		is.images[ID(id)] = imageMeta

		return nil
	})
	if err != nil {
		return err
	}

	// Second pass to fill in children maps
	for id := range is.images {
		if parent, err := is.GetParent(id); err == nil {
			if parentMeta := is.images[parent]; parentMeta != nil {
				parentMeta.children[id] = struct{}{}
			}
		}
	}

	return nil
}

func (is *store) Create(config []byte) (ID, error) {
	var img Image
	err := json.Unmarshal(config, &img)
	if err != nil {
		return "", err
	}

	// Must reject any config that references diffIDs from the history
	// which aren't among the rootfs layers.
	rootFSLayers := make(map[layer.DiffID]struct{})
	for _, diffID := range img.RootFS.DiffIDs {
		rootFSLayers[diffID] = struct{}{}
	}

	layerCounter := 0
	for _, h := range img.History {
		if !h.EmptyLayer {
			layerCounter++
		}
	}
	if layerCounter > len(img.RootFS.DiffIDs) {
		return "", errors.New("too many non-empty layers in History section")
	}

	is.Lock()
	defer is.Unlock()

	dgst, err := is.fs.Set(config)
	if err != nil {
		return "", err
	}
	imageID := ID(dgst)

	if _, exists := is.images[imageID]; exists {
		return imageID, nil
	}

	layerID := img.RootFS.ChainID()

	var l layer.Layer
	if layerID != "" {
		l, err = is.ls.Get(layerID)
		if err != nil {
			return "", err
		}
	}

	imageMeta := &imageMeta{
		layer:          l,
		children:       make(map[ID]struct{}),
		referenceCount: 0,
	}

	is.images[imageID] = imageMeta
	if err := is.digestSet.Add(digest.Digest(imageID)); err != nil {
		delete(is.images, imageID)
		return "", err
	}

	return imageID, nil
}

func (is *store) Search(term string) (ID, error) {
	is.Lock()
	defer is.Unlock()

	dgst, err := is.digestSet.Lookup(term)
	if err != nil {
		if err == digest.ErrDigestNotFound {
			err = fmt.Errorf("No such image: %s", term)
		}
		return "", err
	}
	return ID(dgst), nil
}

func (is *store) Get(id ID) (*Image, error) {
	// todo: Check if image is in images
	// todo: Detect manual insertions and start using them
	config, err := is.fs.Get(id)
	if err != nil {
		return nil, err
	}

	img, err := NewFromJSON(config)
	if err != nil {
		return nil, err
	}
	img.computedID = id

	img.Parent, err = is.GetParent(id)
	if err != nil {
		img.Parent = ""
	}

	return img, nil
}

func (is *store) Delete(id ID) ([]layer.Metadata, error) {
	is.Lock()
	defer is.Unlock()
	return is.delete(id)
}
func (is *store) delete(id ID) ([]layer.Metadata, error) {

	imageMeta := is.images[id]
	if imageMeta == nil {
		return nil, fmt.Errorf("unrecognized image ID %s", id.String())
	}

	if imageMeta.referenceCount > 0 {
		return nil, ErrReferenceCountNotZero
	}
	for id := range imageMeta.children {
		is.fs.DeleteMetadata(id, "parent")
	}
	if parent, err := is.GetParent(id); err == nil && is.images[parent] != nil {
		delete(is.images[parent].children, id)
	}

	if err := is.digestSet.Remove(digest.Digest(id)); err != nil {
		logrus.Errorf("error removing %s from digest set: %q", id, err)
	}
	delete(is.images, id)
	is.fs.Delete(id)
	is.deleteCicMapping(id)

	if imageMeta.layer != nil {
		return is.ls.Release(imageMeta.layer)
	}

	return nil, nil
}

// HoldOn will increase the reference count for image
func (is *store) HoldOn(id ID) error {
	is.Lock()
	defer is.Unlock()

	imageMeta := is.images[id]
	if imageMeta == nil {
		return fmt.Errorf("unrecognized image ID %s", id.String())
	}
	imageMeta.referenceCount++

	if parent, err := is.GetParent(id); err == nil {
		if parentMeta := is.images[parent]; parentMeta != nil {
			parentMeta.referenceCount++
		}
	}

	return nil
}

// HoldOff will decrease the reference count for image
func (is *store) HoldOff(id ID) error {
	is.Lock()
	defer is.Unlock()

	imageMeta := is.images[id]
	if imageMeta == nil {
		return fmt.Errorf("unrecognized image ID %s", id.String())
	}

	imageMeta.referenceCount--
	if parent, err := is.GetParent(id); err == nil {
		if parentMeta := is.images[parent]; parentMeta != nil {
			parentMeta.referenceCount--
		}
	}

	return nil
}

func (is *store) SetParent(id, parent ID) error {
	is.Lock()
	defer is.Unlock()
	parentMeta := is.images[parent]
	if parentMeta == nil {
		return fmt.Errorf("unknown parent image ID %s", parent.String())
	}
	if parent, err := is.GetParent(id); err == nil && is.images[parent] != nil {
		delete(is.images[parent].children, id)
	}
	parentMeta.children[id] = struct{}{}
	return is.fs.SetMetadata(id, "parent", []byte(parent))
}

func (is *store) GetParent(id ID) (ID, error) {
	d, err := is.fs.GetMetadata(id, "parent")
	if err != nil {
		return "", err
	}
	return ID(d), nil // todo: validate?
}

func (is *store) Children(id ID) []ID {
	is.Lock()
	defer is.Unlock()

	return is.children(id)
}

func (is *store) children(id ID) []ID {
	var ids []ID
	if is.images[id] != nil {
		for id := range is.images[id].children {
			ids = append(ids, id)
		}
	}
	return ids
}

func (is *store) Heads() map[ID]*Image {
	return is.imagesMap(false)
}

func (is *store) Map() map[ID]*Image {
	return is.imagesMap(true)
}

func (is *store) imagesMap(all bool) map[ID]*Image {
	is.Lock()
	defer is.Unlock()

	images := make(map[ID]*Image)

	for id := range is.images {
		if !all && len(is.children(id)) > 0 {
			continue
		}
		img, err := is.Get(id)
		if err != nil {
			logrus.Errorf("invalid image access: %q, error: %q", id, err)
			continue
		}
		images[id] = img
	}
	return images
}

// FindCicID find cached ID of combined image by the key.
// See store.cic for key format.
func (is *store) FindCicID(key string) ID {
	is.Lock()
	defer is.Unlock()
	if id, ok := is.cic[key]; ok {
		if _, ok := is.images[id]; ok {
			return id
		}
	}
	return ""
}

func (is *store) AddCicMapping(key string, id ID) {
	is.Lock()
	defer is.Unlock()
	is.cic[key] = id
}

func (is *store) deleteCicMapping(id ID) {
	for k, v := range is.cic {
		if v == id {
			delete(is.cic, k)
		}
	}
}

func (is *store) GenCicKey(ids []ID) (key string) {
	for _, id := range ids {
		key = key + "-" + id.String()
	}

	return key
}
