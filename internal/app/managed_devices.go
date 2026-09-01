package app

import (
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/types"
)

// ManagedDeviceInput 是设备登记输入。
type ManagedDeviceInput struct {
	Alias              string
	Target             RemoteTarget
	Tags               []string
	ProvisionedVersion string
}

// ListManagedDevices 返回当前发起端登记的远端设备。
func ListManagedDevices() ([]types.ManagedDevice, error) {
	return config.ListManagedDevices()
}

// RegisterManagedDevice 登记或更新一台远端设备。
func RegisterManagedDevice(in ManagedDeviceInput) (types.ManagedDevice, error) {
	alias := strings.TrimSpace(in.Alias)
	if alias == "" {
		alias = in.Target.DisplayName()
	}
	if err := in.Target.validate(); err != nil {
		return types.ManagedDevice{}, err
	}
	return config.RegisterManagedDevice(types.ManagedDevice{
		Alias:              alias,
		SSHTarget:          in.Target.SSHDestination(),
		ManagementListen:   RemoteProvisionListen,
		Tags:               in.Tags,
		ProvisionedVersion: in.ProvisionedVersion,
	})
}

// RemoveManagedDevice 只移除本机登记，不卸载远端程序。
func RemoveManagedDevice(alias string) (bool, error) {
	return config.RemoveManagedDevice(alias)
}
