package s3modelprovider

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	log "github.com/sirupsen/logrus"

	"github.com/mKaloer/TFServingCache/pkg/cachemanager"
)

type S3Location struct {
	Bucket string
	// The model "folder" on s3
	KeyPrefix string
}

type S3ModelProvider struct {
	session      *session.Session
	downloader   *s3manager.Downloader
	s3           *s3.S3
	Bucket       string
	ModelBaseDir string
}

func NewS3ModelProvider(bucket string, modelBaseDir string) (*S3ModelProvider, error) {
	sess, err := session.NewSession()
	if err != nil {
		log.WithError(err).Error("Could not create S3 session")
		return nil, err
	}

	downloader := s3manager.NewDownloader(sess)
	s3 := s3.New(sess)
	provider := &S3ModelProvider{
		session:      sess,
		downloader:   downloader,
		s3:           s3,
		Bucket:       bucket,
		ModelBaseDir: modelBaseDir,
	}
	return provider, nil
}

func (provider S3ModelProvider) LoadModel(modelName string, modelVersion int64, destinationDir string) (*cachemanager.Model, error) {
	log.Infof("Fetching model from S3 %s:%d", modelName, modelVersion)
	modelLocation := provider.getKeyForModel(modelName, modelVersion)

	destPath := path.Join(destinationDir, modelName, strconv.FormatInt(modelVersion, 10))
	err := os.MkdirAll(destPath, os.ModeDir)
	if err != nil {
		log.WithError(err).Errorf("Could not create model dir: %s", destPath)
		return nil, err
	}

	totalSize := int64(0)

	downloadObjFunc := func(relativeKey string, obj *s3.Object) error {
		if strings.Contains(relativeKey, "/") {
			// Make sure dir is created
			paths := strings.Split(relativeKey, "/")
			objFolder := path.Join(append([]string{destPath}, paths[:len(paths)-1]...)...)
			err := os.MkdirAll(objFolder, os.ModeDir)
			if err != nil {
				log.WithError(err).Errorf("Could not create object dir: %s", objFolder)
				return err
			}
		}
		// Download to file
		fname := path.Join(destPath, relativeKey)
		f, err := os.Create(fname)
		if err != nil {
			log.WithError(err).Errorf("Could not create object file: %s", fname)
			return err
		}
		sizeOnDisk, err := provider.downloader.Download(f, &s3.GetObjectInput{
			Bucket: &modelLocation.Bucket,
			Key:    obj.Key,
		})
		if err != nil {
			log.WithError(err).Errorf("Could not download object file: %s", *obj.Key)
			return err
		}
		totalSize += sizeOnDisk

		return nil
	}

	err = provider.modelObjectApply(modelLocation, downloadObjFunc)
	if err != nil {
		log.WithError(err).Errorf("Could not download model: %s:%d", modelName, modelVersion)
		return nil, err
	}

	return &cachemanager.Model{
		Identifier: cachemanager.ModelIdentifier{ModelName: modelName, Version: modelVersion},
		Path:       path.Join(modelName, strconv.FormatInt(modelVersion, 10)),
		SizeOnDisk: totalSize,
	}, nil
}

func (provider S3ModelProvider) ModelSize(modelName string, modelVersion int64) (int64, error) {
	modelLocation := provider.getKeyForModel(modelName, modelVersion)
	totalSize := int64(0)
	countSizeFunc := func(relativeKey string, obj *s3.Object) error {
		totalSize += *obj.Size
		return nil
	}

	err := provider.modelObjectApply(modelLocation, countSizeFunc)
	if err != nil {
		log.WithError(err).Errorf("Could not get model size: %s:%d", modelName, modelVersion)
		return 0, err
	}
	return totalSize, nil
}

func (provider *S3ModelProvider) modelObjectApply(modelLocation S3Location,
	applyFun func(string, *s3.Object) error) error {
	// Download from s3
	isTruncated := true
	var continuationToken *string = nil
	for isTruncated {
		modelObjects, err := provider.s3.ListObjectsV2(&s3.ListObjectsV2Input{
			Bucket:            &modelLocation.Bucket,
			Prefix:            &modelLocation.KeyPrefix,
			ContinuationToken: continuationToken,
		})

		if err != nil {
			log.WithError(err).Errorf("Error accessing model on S3. Bucket: %s, keyPrefix: %s", modelLocation.Bucket, modelLocation.KeyPrefix)
			return err
		}

		// download files
		for _, object := range modelObjects.Contents {

			relativeName := strings.TrimPrefix(*object.Key, modelLocation.KeyPrefix)
			if !strings.HasSuffix(relativeName, "/") {
				// Is not a folder
				err = applyFun(relativeName, object)
				if err != nil {
					log.WithError(err).Errorf("Apply func returned error on key: %s", *object.Key)
					return err
				}
			}
		}

		isTruncated = *modelObjects.IsTruncated
		continuationToken = modelObjects.ContinuationToken
	}
	return nil
}

func (provider S3ModelProvider) getKeyForModel(modelName string, modelVersion int64) S3Location {
	return S3Location{
		Bucket:    provider.Bucket,
		KeyPrefix: fmt.Sprintf("%s/%s/%d/", provider.ModelBaseDir, modelName, modelVersion),
	}
}
