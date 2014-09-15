package cdrom

import (
	"os"
	"path/filepath"

	bosherr "github.com/cloudfoundry/bosh-agent/errors"
	boshdevutil "github.com/cloudfoundry/bosh-agent/platform/deviceutil"
	boshsys "github.com/cloudfoundry/bosh-agent/system"
)

type cdUtil struct {
	settingsMountPath string
	fs                boshsys.FileSystem
	cdrom             Cdrom
}

func NewCdUtil(settingsMountPath string, fs boshsys.FileSystem, cdrom Cdrom) boshdevutil.DeviceUtil {
	return cdUtil{
		settingsMountPath: settingsMountPath,
		fs:                fs,
		cdrom:             cdrom,
	}
}

func (util cdUtil) GetFileContents(fileName string) (contents []byte, err error) {
	err = util.cdrom.WaitForMedia()
	if err != nil {
		err = bosherr.WrapError(err, "Waiting for CDROM to be ready")
		return
	}

	err = util.fs.MkdirAll(util.settingsMountPath, os.FileMode(0700))
	if err != nil {
		err = bosherr.WrapError(err, "Creating CDROM mount point")
		return
	}

	err = util.cdrom.Mount(util.settingsMountPath)
	if err != nil {
		err = bosherr.WrapError(err, "Mounting CDROM")
		return
	}

	settingsPath := filepath.Join(util.settingsMountPath, fileName)
	stringContents, err := util.fs.ReadFile(settingsPath)
	if err != nil {
		err = bosherr.WrapError(err, "Reading from CDROM")
		return
	}

	err = util.cdrom.Unmount()
	if err != nil {
		err = bosherr.WrapError(err, "Unmounting CDROM")
		return
	}

	err = util.cdrom.Eject()
	if err != nil {
		err = bosherr.WrapError(err, "Ejecting CDROM")
		return
	}

	contents = []byte(stringContents)
	return
}