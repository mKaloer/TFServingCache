package azblobmodelprovider

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/Azure/azure-pipeline-go/pipeline"
	"github.com/Azure/azure-storage-blob-go/azblob"
	log "github.com/sirupsen/logrus"

	"github.com/mKaloer/TFServingCache/pkg/cachemanager"
)

type AZBlobLocation struct {
	// The model "folder" on the azure container
	KeyPrefix string
}

type AZBlobModelProvider struct {
	credential   *azblob.SharedKeyCredential
	pipeline     pipeline.Pipeline
	ContainerURL *url.URL
	ModelBaseDir string
}

func NewAZBlobModelProvider(container string, modelBaseDir string, accountName string, accountKey string) (*AZBlobModelProvider, error) {
	containerUrl := fmt.Sprintf("https://%s.blob.core.windows.net/%s", accountName, container)
	return NewAZBlobModelProviderWithUrl(containerUrl, modelBaseDir, accountKey, accountKey)
}

func NewAZBlobModelProviderWithUrl(containerUrl string, modelBaseDir string, accountName string, accountKey string) (*AZBlobModelProvider, error) {
	credential, err := azblob.NewSharedKeyCredential(accountName, accountKey)
	if err != nil {
		log.WithError(err).Error("Could not create AZ blob session")
		return nil, err
	}
	pipeline := azblob.NewPipeline(credential, azblob.PipelineOptions{})

	url, err := url.Parse(containerUrl)
	if err != nil {
		log.WithError(err).Error("Could not parse storage url")
		return nil, err
	}

	provider := &AZBlobModelProvider{
		credential:   credential,
		pipeline:     pipeline,
		ContainerURL: url,
		ModelBaseDir: modelBaseDir,
	}
	return provider, nil
}

func (provider AZBlobModelProvider) LoadModel(modelName string, modelVersion int64, destinationDir string) (*cachemanager.Model, error) {
	log.Infof("Fetching model from AZ Blob %s:%d", modelName, modelVersion)
	modelLocation := provider.getKeyForModel(modelName, modelVersion)

	destPath := path.Join(destinationDir, modelName, strconv.FormatInt(modelVersion, 10))
	err := os.MkdirAll(destPath, 0777)
	if err != nil {
		log.WithError(err).Errorf("Could not create model dir: %s", destPath)
		return nil, err
	}

	totalSize := int64(0)

	downloadFunc := func(relativeName string, blob *azblob.BlobItemInternal, url *url.URL) error {
		paths := strings.Split(relativeName, "/")
		objFolder := path.Join(append([]string{destPath}, paths[:len(paths)-1]...)...)
		err := os.MkdirAll(objFolder, 0777)
		if err != nil {
			log.WithError(err).Errorf("Could not create object dir: %s", objFolder)
			return nil
		}
		fname := path.Join(destPath, relativeName)
		f, err := os.Create(fname)
		if err != nil {
			log.WithError(err).Errorf("Could not create object file: %s", fname)
			return nil
		}
		err = azblob.DownloadBlobToFile(context.Background(), azblob.NewBlobURL(*url, provider.pipeline), 0, 0, f, azblob.DownloadFromBlobOptions{})
		if err != nil {
			log.WithError(err).Errorf("Could not download object file: %s", fname)
			return nil
		}
		totalSize += *blob.Properties.ContentLength
		return nil
	}
	err = provider.modelObjectApply(modelLocation, downloadFunc)
	if err != nil {
		log.WithError(err).Errorf("Could not download model: %s", modelLocation.KeyPrefix)
		return nil, err
	}

	return &cachemanager.Model{
		Identifier: cachemanager.ModelIdentifier{ModelName: modelName, Version: modelVersion},
		Path:       path.Join(modelName, strconv.FormatInt(modelVersion, 10)),
		SizeOnDisk: totalSize,
	}, nil
}

func (provider AZBlobModelProvider) ModelSize(modelName string, modelVersion int64) (int64, error) {
	modelLocation := provider.getKeyForModel(modelName, modelVersion)
	totalSize := int64(0)
	countSizeFunc := func(relativeKey string, obj *azblob.BlobItemInternal, url *url.URL) error {
		totalSize += *obj.Properties.ContentLength
		return nil
	}

	err := provider.modelObjectApply(modelLocation, countSizeFunc)
	if err != nil {
		log.WithError(err).Errorf("Could not get model size: %s:%d", modelName, modelVersion)
		return 0, err
	}
	return totalSize, nil
}

func (provider *AZBlobModelProvider) modelObjectApply(modelLocation AZBlobLocation,
	applyFun func(string, *azblob.BlobItemInternal, *url.URL) error) error {
	containerURL := azblob.NewContainerURL(*provider.ContainerURL, provider.pipeline)
	ctx := context.Background()
	// List all blobs matching prefix
	for marker := (azblob.Marker{}); marker.NotDone(); {
		// Fetch next segment
		blobs, err := containerURL.ListBlobsFlatSegment(ctx, marker, azblob.ListBlobsSegmentOptions{Prefix: modelLocation.KeyPrefix})
		if err != nil {
			log.WithError(err).Errorf("Could not list blobs: %s", modelLocation.KeyPrefix)
			return err
		}

		for _, blobInfo := range blobs.Segment.BlobItems {
			// Blob Name
			log.Debugf("Blob name: %s", blobInfo.Name)
			blobUrl, _ := url.Parse(fmt.Sprintf("%s/%s", provider.ContainerURL.String(), blobInfo.Name))

			relativeName := strings.TrimPrefix(blobInfo.Name, modelLocation.KeyPrefix)
			if !strings.HasSuffix(relativeName, "/") {
				// Is not a folder
				err = applyFun(relativeName, &blobInfo, blobUrl)
				if err != nil {
					log.WithError(err).Errorf("Apply func returned error on key: %s", blobInfo.Name)
					return err
				}
			}
		}
		marker = blobs.NextMarker
	}

	return nil
}

func (provider AZBlobModelProvider) getKeyForModel(modelName string, modelVersion int64) AZBlobLocation {
	modelPrefix := provider.ModelBaseDir
	if len(modelPrefix) > 0 {
		modelPrefix = fmt.Sprintf("%s/", modelPrefix)
	}
	return AZBlobLocation{
		KeyPrefix: fmt.Sprintf("%s%s/%d/", modelPrefix, modelName, modelVersion),
	}
}
