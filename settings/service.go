package settings

import (
	"encoding/json"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
	"sync"
)

type Service interface {
	LoadSettings() error

	// GetSettings does not return error because without settings Agent cannot start.
	GetSettings() Settings

	GetPersistentDiskSettings(diskCID string) (DiskSettings, error)

	GetAllPersistentDiskSettings() (map[string]DiskSettings, error)

	SavePersistentDiskSettings(DiskSettings) error

	RemovePersistentDiskSettings(string) error

	PublicSSHKeyForUsername(string) (string, error)

	InvalidateSettings() error
}

const settingsServiceLogTag = "settingsService"

type settingsService struct {
	fs                          boshsys.FileSystem
	settingsPath                string
	settings                    Settings
	settingsMutex               sync.Mutex
	persistentDiskSettingsPath  string
	persistentDiskSettingsMutex sync.Mutex
	settingsSource              Source
	defaultNetworkResolver      DefaultNetworkResolver
	logger                      boshlog.Logger
}

type DefaultNetworkResolver interface {
	// Ideally we would find a network based on a MAC address
	// but current CPI implementations do not include it
	GetDefaultNetwork() (Network, error)
}

func NewService(
	fs boshsys.FileSystem,
	settingsPath string,
	persistentDiskSettingsPath string,
	settingsSource Source,
	defaultNetworkResolver DefaultNetworkResolver,
	logger boshlog.Logger,
) Service {
	return &settingsService{
		fs:                         fs,
		settingsPath:               settingsPath,
		settings:                   Settings{},
		persistentDiskSettingsPath: persistentDiskSettingsPath,
		settingsSource:             settingsSource,
		defaultNetworkResolver:     defaultNetworkResolver,
		logger:                     logger,
	}
}

func (s *settingsService) PublicSSHKeyForUsername(username string) (string, error) {
	return s.settingsSource.PublicSSHKeyForUsername(username)
}

func (s *settingsService) LoadSettings() error {
	s.logger.Debug(settingsServiceLogTag, "Loading settings from fetcher")

	newSettings, fetchErr := s.settingsSource.Settings()
	if fetchErr != nil {
		s.logger.Error(settingsServiceLogTag, "Failed loading settings via fetcher: %v", fetchErr)

		opts := boshsys.ReadOpts{Quiet: true}
		existingSettingsJSON, readError := s.fs.ReadFileWithOpts(s.settingsPath, opts)
		if readError != nil {
			s.logger.Error(settingsServiceLogTag, "Failed reading settings from file %s", readError.Error())
			return bosherr.WrapError(fetchErr, "Invoking settings fetcher")
		}

		s.logger.Debug(settingsServiceLogTag, "Successfully read settings from file")

		cachedSettings := Settings{}

		err := json.Unmarshal(existingSettingsJSON, &cachedSettings)
		if err != nil {
			s.logger.Error(settingsServiceLogTag, "Failed unmarshalling settings from file %s", err.Error())
			return bosherr.WrapError(fetchErr, "Invoking settings fetcher")
		}

		s.settingsMutex.Lock()
		s.settings = cachedSettings
		s.settingsMutex.Unlock()

		return nil
	}

	s.logger.Debug(settingsServiceLogTag, "Successfully received settings from fetcher")
	s.settingsMutex.Lock()
	s.settings = newSettings
	s.settingsMutex.Unlock()

	newSettingsJSON, err := json.Marshal(newSettings)
	if err != nil {
		return bosherr.WrapError(err, "Marshalling settings json")
	}

	err = s.fs.WriteFileQuietly(s.settingsPath, newSettingsJSON)
	if err != nil {
		return bosherr.WrapError(err, "Writing setting json")
	}

	return nil
}

func (s *settingsService) GetAllPersistentDiskSettings() (map[string]DiskSettings, error) {
	s.persistentDiskSettingsMutex.Lock()
	defer s.persistentDiskSettingsMutex.Unlock()

	allPersistentDiskSettings := map[string]DiskSettings{}

	settings := s.GetSettings()

	for diskCID, settings := range settings.Disks.Persistent {
		allPersistentDiskSettings[diskCID] = s.settings.populatePersistentDiskSettings(diskCID, settings)
	}

	persistentDiskSettings, err := s.getPersistentDiskSettingsWithoutLocking()
	if err != nil {
		return nil, bosherr.WrapError(err, "Reading persistent disk settings")
	}

	for diskCID, settings := range persistentDiskSettings {
		allPersistentDiskSettings[diskCID] = settings
	}

	return allPersistentDiskSettings, nil
}

func (s *settingsService) GetPersistentDiskSettings(diskCID string) (DiskSettings, error) {
	allDiskSettings, err := s.GetAllPersistentDiskSettings()
	if err != nil {
		return DiskSettings{}, bosherr.WrapError(err, "Getting all persistent disk settings")
	}

	settings, hasDiskSettings := allDiskSettings[diskCID]
	if !hasDiskSettings {
		return DiskSettings{}, bosherr.Errorf("Persistent disk with volume id '%s' could not be found", diskCID)
	}

	return settings, nil
}

func (s *settingsService) RemovePersistentDiskSettings(diskID string) error {
	s.persistentDiskSettingsMutex.Lock()
	defer s.persistentDiskSettingsMutex.Unlock()

	persistentDiskSettings, err := s.getPersistentDiskSettingsWithoutLocking()
	if err != nil {
		return bosherr.WrapError(err, "Cannot remove entry from file due to read error")
	}

	delete(persistentDiskSettings, diskID)
	if err := s.savePersistentDiskSettingsWithoutLocking(persistentDiskSettings); err != nil {
		return bosherr.WrapError(err, "Saving persistent disk settings")
	}

	return nil
}

func (s *settingsService) SavePersistentDiskSettings(newDiskSettings DiskSettings) error {
	s.persistentDiskSettingsMutex.Lock()
	defer s.persistentDiskSettingsMutex.Unlock()

	persistentDiskSettings, err := s.getPersistentDiskSettingsWithoutLocking()
	if err != nil {
		return bosherr.WrapError(err, "Reading all persistent disk settings")
	}

	persistentDiskSettings[newDiskSettings.ID] = newDiskSettings
	if err := s.savePersistentDiskSettingsWithoutLocking(persistentDiskSettings); err != nil {
		return bosherr.WrapError(err, "Saving persistent disk settings")
	}

	return nil
}

// GetSettings returns setting even if it fails to resolve IPs for dynamic networks.
func (s *settingsService) GetSettings() Settings {
	s.settingsMutex.Lock()

	settingsCopy := s.settings

	if s.settings.Networks != nil {
		settingsCopy.Networks = make(map[string]Network)
	}

	for networkName, network := range s.settings.Networks {
		settingsCopy.Networks[networkName] = network
	}
	s.settingsMutex.Unlock()

	if settingsCopy.Networks.HasInterfaceAlias() {
		return settingsCopy
	}

	for networkName, network := range settingsCopy.Networks {
		if !network.IsDHCP() {
			continue
		}

		resolvedNetwork, err := s.resolveNetwork(network)
		if err != nil {
			break
		}

		settingsCopy.Networks[networkName] = resolvedNetwork
	}
	return settingsCopy
}

func (s *settingsService) InvalidateSettings() error {
	err := s.fs.RemoveAll(s.settingsPath)
	if err != nil {
		return bosherr.WrapError(err, "Removing settings file")
	}

	return nil
}

func (s *settingsService) resolveNetwork(network Network) (Network, error) {
	// Ideally this would be GetNetworkByMACAddress(mac string)
	// Currently, we are relying that if the default network does not contain
	// the MAC adddress the InterfaceConfigurationCreator will fail.
	resolvedNetwork, err := s.defaultNetworkResolver.GetDefaultNetwork()
	if err != nil {
		s.logger.Error(settingsServiceLogTag, "Failed retrieving default network %s", err.Error())
		return Network{}, bosherr.WrapError(err, "Failed retrieving default network")
	}

	// resolvedNetwork does not have all information for a network
	network.IP = resolvedNetwork.IP
	network.Netmask = resolvedNetwork.Netmask
	network.Gateway = resolvedNetwork.Gateway
	network.Resolved = true

	return network, nil
}

func (s *settingsService) savePersistentDiskSettingsWithoutLocking(persistentDiskSettings map[string]DiskSettings) error {
	newPersistentDiskSettingsJSON, err := json.Marshal(persistentDiskSettings)
	if err != nil {
		return bosherr.WrapError(err, "Marshalling persistent disk settings json")
	}

	err = s.fs.WriteFile(s.persistentDiskSettingsPath, newPersistentDiskSettingsJSON)
	if err != nil {
		return bosherr.WrapError(err, "Writing persistent disk settings settings json")
	}

	return nil
}

func (s *settingsService) getPersistentDiskSettingsWithoutLocking() (map[string]DiskSettings, error) {
	persistentDiskSettings := make(map[string]DiskSettings)

	if s.fs.FileExists(s.persistentDiskSettingsPath) {
		opts := boshsys.ReadOpts{Quiet: true}
		existingSettingsJSON, readError := s.fs.ReadFileWithOpts(s.persistentDiskSettingsPath, opts)
		if readError != nil {
			return nil, bosherr.WrapError(readError, "Reading persistent disk settings from file")
		}

		err := json.Unmarshal(existingSettingsJSON, &persistentDiskSettings)
		if err != nil {
			return nil, bosherr.WrapError(err, "Unmarshalling persistent disk settings from file")
		}
	}
	return persistentDiskSettings, nil
}
