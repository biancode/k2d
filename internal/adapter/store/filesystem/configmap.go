package filesystem

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/portainer/k2d/internal/adapter/store/errors"
	"github.com/portainer/k2d/pkg/filesystem"
	"github.com/portainer/k2d/pkg/maputils"
	str "github.com/portainer/k2d/pkg/strings"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/apis/core"
)

// TODO: add function comments

// TODO: introduce a naming package in each store implementation to centralize the naming logic

// Each configmap has its own metadata file using the following naming convention:
// [namespace]-[configmap-name]-k2dcm.metadata
func buildConfigMapMetadataFileName(configMapName, namespace string) string {
	return fmt.Sprintf("%s-%s-k2dcm.metadata", namespace, configMapName)
}

// Each key of a configmap is stored in a separate file using the following naming convention:
// [namespace]-[configmap-name]-k2dcm-[key]
func buildConfigMapFilePrefix(configMapName, namespace string) string {
	return fmt.Sprintf("%s-%s%s", namespace, configMapName, ConfigMapSeparator)
}

func (s *FileSystemStore) DeleteConfigMap(configMapName, namespace string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	files, err := os.ReadDir(s.configMapPath)
	if err != nil {
		return fmt.Errorf("unable to read configmap directory: %w", err)
	}

	filePrefix := buildConfigMapFilePrefix(configMapName, namespace)

	// TODO: centralize this logic into a function hasMatchingConfigMapFile(files []os.FileInfo, filePrefix string) bool
	hasMatchingConfigMapFile := false
	for _, file := range files {
		if strings.HasPrefix(file.Name(), filePrefix) {
			hasMatchingConfigMapFile = true
			break
		}
	}

	if !hasMatchingConfigMapFile {
		return errors.ErrResourceNotFound
	}

	metadataFileName := buildConfigMapMetadataFileName(configMapName, namespace)
	err = os.Remove(path.Join(s.configMapPath, metadataFileName))
	if err != nil {
		return fmt.Errorf("unable to remove configmap metadata file %s: %w", metadataFileName, err)
	}

	for _, file := range files {
		if strings.HasPrefix(file.Name(), filePrefix) {
			err := os.Remove(path.Join(s.configMapPath, file.Name()))
			if err != nil {
				return fmt.Errorf("unable to remove configmap data file %s: %w", file.Name(), err)
			}
		}
	}

	return nil
}

// The filesystem implementation will return a list of files that needs to be mounted
// for a specific ConfigMap. This list is built from the store.k2d.io/filesystem/path/* annotations of the ConfigMap.
// Each bind contains the filename of the file to mount inside the container and the path to the file on the host.
// The format of each bind is: filename:/path/to/matching/file
func (s *FileSystemStore) GetConfigMapBinds(configMap *core.ConfigMap) (map[string]string, error) {
	binds := map[string]string{}

	for key, value := range configMap.Annotations {
		if strings.HasPrefix(key, FilePathAnnotationKey) {
			binds[strings.TrimPrefix(key, FilePathAnnotationKey+"/")] = value
		}
	}

	return binds, nil
}

// In order to find a configMap, we need to list all the files in the configmap directory
// This will return something like this:
// default-app-config-k2dcm-APP_SETTING  default-app-config-k2dcm-APP_UI_SETTING
// We then need to validate that the map that we are looking for have at least one corresponding file
// If not we return an error not found
// To verify that we have at least one file matching the configmap name,
func (s *FileSystemStore) GetConfigMap(configMapName, namespace string) (*core.ConfigMap, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	files, err := os.ReadDir(s.configMapPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read configmap directory: %w", err)
	}

	filePrefix := buildConfigMapFilePrefix(configMapName, namespace)

	hasMatchingConfigMapFile := false
	for _, file := range files {
		if strings.HasPrefix(file.Name(), filePrefix) {
			hasMatchingConfigMapFile = true
			break
		}
	}

	if !hasMatchingConfigMapFile {
		return nil, errors.ErrResourceNotFound
	}

	configMap := core.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        configMapName,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},
		Data: map[string]string{},
	}

	metadataFileName := buildConfigMapMetadataFileName(configMapName, namespace)
	metadata, err := filesystem.LoadMetadataFromDisk(s.configMapPath, metadataFileName)
	if err != nil {
		return nil, fmt.Errorf("unable to load configmap metadata from disk: %w", err)
	}

	configMap.Labels = metadata

	for _, file := range files {
		if strings.HasPrefix(file.Name(), filePrefix) {
			data, err := os.ReadFile(path.Join(s.configMapPath, file.Name()))
			if err != nil {
				return nil, fmt.Errorf("unable to read file %s: %w", file.Name(), err)
			}

			configMap.Data[strings.TrimPrefix(file.Name(), filePrefix)] = string(data)

			// TODO: instead of relying on os.Stat for the creation timestamp, we should store it in the metadata file
			// when the configmap is created as a unix timestamp
			info, err := os.Stat(path.Join(s.configMapPath, file.Name()))
			if err != nil {
				return nil, fmt.Errorf("unable to get file info for %s: %w", file.Name(), err)
			}

			configMap.ObjectMeta.CreationTimestamp = metav1.NewTime(info.ModTime())

			// The path to the file is stored in the annotation so that it can be mounted
			// inside a container by reading the store.k2d.io/filesystem/path/* annotations.
			// See the GetConfigMapBinds function for more details.
			configMap.ObjectMeta.Annotations[fmt.Sprintf("%s/%s", FilePathAnnotationKey, strings.TrimPrefix(file.Name(), filePrefix))] = path.Join(s.configMapPath, file.Name())
		}
	}

	return &configMap, nil
}

func (s *FileSystemStore) GetConfigMaps(namespace string) (core.ConfigMapList, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	files, err := os.ReadDir(s.configMapPath)
	if err != nil {
		return core.ConfigMapList{}, fmt.Errorf("unable to read configmap directory: %w", err)
	}

	fileNames := []string{}
	for _, file := range files {
		fileNames = append(fileNames, file.Name())
	}

	// We first need to find all the unique configmap names
	uniqueNames := str.UniquePrefixes(fileNames, ConfigMapSeparator)

	// We then need to filter out the configmaps that are not in the namespace
	uniqueNames = str.FilterStringsByPrefix(uniqueNames, namespace)

	// We also need to filter out the metadata files
	uniqueNames = str.RemoveItemsWithSuffix(uniqueNames, ".metadata")

	configMaps := []core.ConfigMap{}
	for _, name := range uniqueNames {

		configMap := core.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{},
			Data:       map[string]string{},
		}

		// We lookup for the metadata file first, it contains the labels associated to the configmap
		// and that includes a specific label that is used to identify the namespace associated to the configmap

		// at this stage name = default-app-config
		// e.g. [namespace]-[configmap-name]

		// TODO: find a better way to do this, this is dirty as it doesn't rely on the buildConfigMapMetadataFileName function
		// Need another naming function
		metadataFileName := fmt.Sprintf("%s-k2dcm.metadata", name)
		metadata, err := filesystem.LoadMetadataFromDisk(s.configMapPath, metadataFileName)
		if err != nil {
			return core.ConfigMapList{}, fmt.Errorf("unable to load configmap metadata from disk: %w", err)
		}

		configMap.Labels = metadata
		configMap.ObjectMeta.Namespace = metadata[NamespaceNameLabelKey]
		configMap.ObjectMeta.Name = strings.TrimPrefix(name, configMap.ObjectMeta.Namespace+"-")

		// We then lookup for the data files and build the data map
		filePrefix := buildConfigMapFilePrefix(configMap.ObjectMeta.Name, configMap.ObjectMeta.Namespace)
		for _, file := range files {
			if strings.HasPrefix(file.Name(), filePrefix) {
				data, err := os.ReadFile(path.Join(s.configMapPath, file.Name()))
				if err != nil {
					return core.ConfigMapList{}, fmt.Errorf("unable to read file %s: %w", file.Name(), err)
				}

				configMap.Data[strings.TrimPrefix(file.Name(), filePrefix)] = string(data)
				info, err := os.Stat(path.Join(s.configMapPath, file.Name()))
				if err != nil {
					return core.ConfigMapList{}, fmt.Errorf("unable to get file info for %s: %w", file.Name(), err)
				}
				configMap.ObjectMeta.CreationTimestamp = metav1.NewTime(info.ModTime())
			}
		}

		configMaps = append(configMaps, configMap)
	}

	return core.ConfigMapList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMapList",
			APIVersion: "v1",
		},
		Items: configMaps,
	}, nil
}

func (s *FileSystemStore) StoreConfigMap(configMap *corev1.ConfigMap) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	labels := map[string]string{
		NamespaceNameLabelKey: configMap.Namespace,
	}
	maputils.MergeMapsInPlace(labels, configMap.Labels)

	metadataFileName := buildConfigMapMetadataFileName(configMap.Name, configMap.Namespace)
	err := filesystem.StoreMetadataOnDisk(s.configMapPath, metadataFileName, labels)
	if err != nil {
		return fmt.Errorf("unable to store configmap metadata on disk: %w", err)
	}

	filePrefix := buildConfigMapFilePrefix(configMap.Name, configMap.Namespace)
	err = filesystem.StoreDataMapOnDisk(s.configMapPath, filePrefix, configMap.Data)
	if err != nil {
		return fmt.Errorf("unable to store configmap data on disk: %w", err)
	}

	return nil
}
