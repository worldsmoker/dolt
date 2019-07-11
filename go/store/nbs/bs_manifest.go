package nbs

import (
	"bytes"
	"context"
	"github.com/liquidata-inc/ld/dolt/go/store/blobstore"
)

const (
	manifestFile = "manifest"
)

type blobstoreManifest struct {
	name string
	bs   blobstore.Blobstore
}

func (bsm blobstoreManifest) Name() string {
	return bsm.name
}

func manifestVersionAndContents(ctx context.Context, bs blobstore.Blobstore) (string, manifestContents, error) {
	reader, ver, err := bs.Get(ctx, manifestFile, blobstore.AllRange)

	if err != nil {
		return "", manifestContents{}, err
	}

	defer reader.Close()
	contents, err := parseManifest(reader)

	if err != nil {
		return "", manifestContents{}, err
	}

	return ver, contents, nil
}

// ParseIfExists looks for a manifest in the specified blobstore.  If one exists
// will return true and the contents, else false and nil
func (bsm blobstoreManifest) ParseIfExists(ctx context.Context, stats *Stats, readHook func() error) (bool, manifestContents, error) {
	if readHook != nil {
		panic("Read hooks not supported")
	}

	_, contents, err := manifestVersionAndContents(ctx, bsm.bs)

	if err != nil {
		if blobstore.IsNotFoundError(err) {
			return false, contents, nil
		}

		// io error
		return true, contents, err
	}

	return true, contents, nil
}

// Update updates the contents of the manifest in the blobstore
func (bsm blobstoreManifest) Update(ctx context.Context, lastLock addr, newContents manifestContents, stats *Stats, writeHook func() error) (manifestContents, error) {
	if writeHook != nil {
		panic("Write hooks not supported")
	}

	ver, contents, err := manifestVersionAndContents(ctx, bsm.bs)

	if err != nil {
		return manifestContents{}, err
	}

	if contents.lock == lastLock {
		buffer := bytes.NewBuffer(make([]byte, 64*1024)[:0])
		err := writeManifest(buffer, newContents)

		if err != nil {
			return manifestContents{}, err
		}

		_, err = bsm.bs.CheckAndPut(ctx, ver, manifestFile, buffer)

		if err != nil {
			if !blobstore.IsCheckAndPutError(err) {
				return manifestContents{}, err
			}
		} else {
			return newContents, nil
		}
	}

	return contents, nil
}