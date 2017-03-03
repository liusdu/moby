package daemon

import (
	"github.com/docker/docker/image"
	"github.com/docker/docker/layer"
	"github.com/docker/docker/reference"
)

// Combine names to default name. If name is a digest, use short ID instead. format like
//  - name1:tag1 name2:tag2    -->    name1_tag1-name2_tag2
// Hub and namespace is stripped. name and tag are connected with separator '_' and nams are
// connected with separator '-'.
func joinNames(name string, next string) (string, error) {
	formated, err := reference.CombinedFormat(name)
	if err != nil {
		return "", err
	}

	if next == "" {
		return formated, nil
	}

	return reference.JoinCombined(formated, next), nil
}

// Combine names to default name, include getting name from `From` field in image.
func (daemon *Daemon) combineName(name string) (jName string, ids []image.ID, err error) {
	flexablePrefix, _, err := reference.SplitName(name)
	if err != nil {
		return "", nil, err
	}

	newNames, ids, err := daemon.getNamesRecursive(name, "", flexablePrefix)
	if err != nil {
		return "", nil, err
	}

	if len(newNames) == 1 {
		return name, ids, nil
	}

	for _, newName := range newNames {
		jName, err = joinNames(newName, jName)
		if err != nil {
			return "", nil, err
		}
	}

	return jName, ids, nil
}

// getNamesRecursive combine image's name and image's parent's name to form a new
// default name for running. It will also get images's parent's IDs for furture use.
// If image can't find locally, it will try to pull image's parent image recursively.
// Parent image is specified by `name` or parsed form
func (daemon *Daemon) getNamesRecursive(name string, id image.ID, flexablePrefix string) (newNames []string, ids []image.ID, err error) {
	newName, err := reference.ReplaceFlexablePrefix(name, flexablePrefix)
	if err != nil {
		return nil, nil, err
	}

	refOrID := name
	if id != "" {
		refOrID = id.String()
	}

	img, err := daemon.GetImage(refOrID)
	if err != nil {
		return nil, nil, err
	}

	imgID := ""
	if img.From != "" {
		name, imgID, err = reference.ParseFrom(img.From)
		if err != nil {
			return nil, nil, err
		}

		newNames, ids, err = daemon.getNamesRecursive(name, image.ID(imgID), flexablePrefix)
		if err != nil {
			return nil, nil, err
		}
	}

	return append(newNames, newName), append(ids, img.ID()), nil
}

func (daemon *Daemon) combineImage(ids []image.ID) (image.ID, error) {
	newImg, err := daemon.GetImage(ids[len(ids)-1].String())
	if err != nil {
		return "", err
	}

	newImg.RootFS = &image.RootFS{}
	newImg.History = []image.History{}

	// Retrive all image's layers and image's parent's as new image's layers.
	// Only layers, history, `From` field are changed, others remain untouched.
	// It only combine all these infomation again to make up the new image, so
	// image will be the same when combine again.
	for imgIdx, id := range ids {
		imgDesc, err := daemon.GetImage(id.String())
		if err != nil {
			return "", err
		}

		for i := range imgDesc.RootFS.DiffIDs {
			rootFS := *imgDesc.RootFS
			rootFS.DiffIDs = rootFS.DiffIDs[:i+1]

			// ChainID of lowest image's layers are same as new image, no need to do register again.
			if imgIdx != 0 {
				l, err := daemon.layerStore.Get(rootFS.ChainID())
				if err != nil {
					return "", err
				}
				defer layer.ReleaseAndLog(daemon.layerStore, l)

				tar, err := l.TarStream()
				if err != nil {
					return "", err
				}
				defer tar.Close()

				newLayer, err := daemon.layerStore.Register(tar, newImg.RootFS.ChainID())
				if err != nil {
					return "", err
				}
				defer layer.ReleaseAndLog(daemon.layerStore, newLayer)
			}

			newImg.RootFS.DiffIDs = append(newImg.RootFS.DiffIDs, rootFS.DiffIDs[i])
		}

		newImg.History = append(newImg.History, imgDesc.History...)
	}

	newImg.From = ""

	newConfig, err := newImg.MarshalJSON()
	if err != nil {
		return "", err
	}

	newImageID, err := daemon.imageStore.Create(newConfig)
	if err != nil {
		return "", err
	}

	daemon.imageStore.AddCicMapping(daemon.imageStore.GenCicKey(ids), newImageID)

	return newImageID, nil
}

// IsCompleteImage check if a image is complete.
//  - true   image is complete
//  - false  image is partial
func (daemon *Daemon) IsCompleteImage(refOrID string) (bool, error) {
	img, err := daemon.GetImage(refOrID)
	if err != nil {
		return true, err
	}

	if img.From == "" {
		return true, nil
	}

	return false, nil
}

// CreateCompleteImage creates a image for running, and then return the new image's id for furture
// use. It will find all it's parent images and combine them into be a complete image.
func (daemon *Daemon) CreateCompleteImage(name string) (defaultName, imageID string, err error) {
	defaultName, ids, err := daemon.combineName(name)
	if err != nil {
		return "", "", err
	}

	// Use cache to save time for preparing running. If cached image found,
	// use cached image directly.
	key := daemon.imageStore.GenCicKey(ids)
	rtImgID := daemon.imageStore.FindCicID(key)
	if rtImgID == "" {
		rtImgID, err = daemon.combineImage(ids)
		if err != nil {
			return "", "", err
		}
	}

	ref, err := reference.WithName(defaultName)
	if err != nil {
		return "", "", err
	}

	ref = reference.WithDefaultTag(ref)

	if err := daemon.TagImage(ref, rtImgID.String()); err != nil {
		return "", "", err
	}

	return defaultName, rtImgID.String(), nil
}

// getImageRecursive retrives all infomation(except parent's `From` field) about the image
// and it's parent(specified by `From` field) recursive, so the result is the complete
// infomation when running this image.
func (daemon *Daemon) getImageRecursive(img image.ID) (*image.Image, error) {
	imgDesc, err := daemon.imageStore.Get(img)
	if err != nil {
		return nil, err
	}

	if imgDesc.From == "" {
		return imgDesc, nil
	}

	_, id, err := reference.ParseFrom(imgDesc.From)
	if err != nil {
		return nil, err
	}

	parentImgDesc, err := daemon.getImageRecursive(image.ID(id))
	if err != nil {
		return nil, err
	}

	imgDesc.RootFS.DiffIDs = append(parentImgDesc.RootFS.DiffIDs, imgDesc.RootFS.DiffIDs...)
	imgDesc.History = append(parentImgDesc.History, imgDesc.History...)

	return imgDesc, nil
}

// CreateNoParentImg use `img` to create a new image without layers in `from` image.
// The result image is only partial, it must run with it's parent image which is specified
// by `From` field.
func (daemon *Daemon) CreateNoParentImg(img string, from string, removeComplete bool) (string, error) {
	imgDesc, err := daemon.imageStore.Get(image.ID(img))
	if err != nil {
		return "", err
	}

	parentID, err := daemon.GetImageID(from)
	if err != nil {
		return "", err
	}

	// If parentID is id of a partial image, then we should also get layers of it's
	// parent's recursively.
	parentImgDesc, err := daemon.getImageRecursive(parentID)
	if err != nil {
		return "", err
	}

	newImg := *imgDesc
	newImg.RootFS = &image.RootFS{}
	newImg.History = []image.History{}

	// This loop retrives layers exist in `img` but not exist in `from`, and then register these
	// layers again to consist new image's layers.
	for i := len(parentImgDesc.RootFS.DiffIDs); i < len(imgDesc.RootFS.DiffIDs); i = i + 1 {
		diffIDs := imgDesc.RootFS.DiffIDs[:i+1]
		l, err := daemon.layerStore.Get(layer.CreateChainID(diffIDs))
		if err != nil {
			return "", err
		}
		defer layer.ReleaseAndLog(daemon.layerStore, l)

		tar, err := l.TarStream()
		if err != nil {
			return "", err
		}
		defer tar.Close()

		newLayer, err := daemon.layerStore.Register(tar, newImg.RootFS.ChainID())
		if err != nil {
			return "", err
		}
		defer layer.ReleaseAndLog(daemon.layerStore, newLayer)
		newImg.RootFS.DiffIDs = append(newImg.RootFS.DiffIDs, diffIDs[i])
	}

	newImg.History = imgDesc.History[len(parentImgDesc.History):]

	_, coreName, err := reference.SplitName(from)
	if err != nil {
		return "", err
	}

	// If `From` field is not nil, it hints that this image is partial.
	// Format looks like "name:tag@digest". For example:
	// "test:v1.0.0@sha256:8a09dc36f9abd977f0e67ce5d85c1a403c72e053508e9dd198e840ec4cd17762"
	// Hub info and namespace info are not included.
	newImg.From = coreName + "@" + parentID.String()

	newConfig, err := newImg.MarshalJSON()
	if err != nil {
		return "", err
	}

	newImageID, err := daemon.imageStore.Create(newConfig)
	if err != nil {
		return "", err
	}

	// This parent infomation is for local image management, it has different
	// meaning from `From` field.
	if err := daemon.imageStore.SetParent(newImageID, parentID); err != nil {
		return "", err
	}

	// `img` is deleted if it does not exists before build. It is't needed if
	// `--no-parent` is specified, only new created partial image is expected.
	if removeComplete {
		if _, err := daemon.imageStore.Delete(image.ID(img)); err != nil {
			return "", err
		}
	}

	return newImageID.String(), nil
}

// GetImages get all images in image store. It used only in building Dockerfile currently.
// In builder we can't use ID and Image struct directly, so we can't use daemon.Map, here
// wrap another function for builder.
// TODO: Maybe we should not get dangling images?
func (daemon *Daemon) GetImages() map[string]struct{} {
	images := make(map[string]struct{})
	imageMap := daemon.imageStore.Map()

	for id := range imageMap {
		images[id.String()] = struct{}{}
	}

	return images
}

// GetCompleteImageFromPartial gets the combined image's default name and ID. Default name's first part
// is id of child image, like 4d86441a0224-busybox_latest:latest. If imageRef is not a partial image, empty
// string will return.
func (daemon *Daemon) GetCompleteImageFromPartial(imageRef string) (string, string, error) {
	complete, err := daemon.IsCompleteImage(imageRef)
	if err != nil {
		return "", "", err
	}

	if complete {
		return "", "", nil
	}

	img, err := daemon.GetImage(imageRef)
	if err != nil {
		return "", "", err
	}

	defaultName, _, err := daemon.combineName(img.ID().String())
	if err != nil {
		return "", "", err
	}

	img, err = daemon.GetImage(defaultName)
	if err != nil {
		return "", "", err
	}

	return defaultName, img.ID().String(), nil
}
