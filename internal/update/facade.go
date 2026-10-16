package update

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	updaterv1 "github.com/shichao402/relkit/api/updater/v1"
	"github.com/shichao402/relkit/sdk"
	"github.com/shichao402/relkit/sdk/updaterfacade"
)

// Set by the canonical artifact producer with:
// -X github.com/shichao402/Dec/internal/update.runtimeBootstrapBase64=<base64>.
var runtimeBootstrapBase64 string

func clientProfile() (*updaterv1.ClientProfile, error) {
	if runtimeBootstrapBase64 == "" {
		return nil, errors.New("Relkit Runtime Profile was not injected at build time")
	}
	raw, err := base64.StdEncoding.DecodeString(runtimeBootstrapBase64)
	if err != nil {
		return nil, fmt.Errorf("decode Relkit runtime bootstrap: %w", err)
	}
	bootstrap, err := sdk.RuntimeBootstrapFromJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse Relkit runtime bootstrap: %w", err)
	}
	return bootstrap.ClientProfile, nil
}

func currentChannel() string {
	channel := config.GetSystemConfig().Channel
	if channel == "" {
		return channelName
	}
	return channel
}

func updaterSidecarPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "relkit-updater"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), name)
}

func suiteRuntime(component, goos, goarch string, currentCode int64, installRoot string) *updaterv1.Runtime {
	name := component
	sidecar := "relkit-updater"
	if goos == "windows" {
		name += ".exe"
		sidecar += ".exe"
	}
	dataDir, err := stateDir()
	if err != nil {
		dataDir = installRoot
	}
	dataDir = filepath.Join(dataDir, "updater", goos+"-"+goarch+"-"+component)
	return &updaterv1.Runtime{
		Channel:     currentChannel(),
		CurrentCode: currentCode,
		ClientSelectors: map[string]string{
			"os":        goos,
			"arch":      goarch,
			"component": component,
			"audience":  "runtime",
		},
		DataDir: dataDir,
		Install: &updaterv1.InstallSpec{
			Placement:         updaterv1.Placement_PLACEMENT_IN_PLACE,
			InstallRoot:       installRoot,
			ExecutableRelpath: name,
			SidecarRelpath:    sidecar,
		},
		SidecarPath: updaterSidecarPath(),
	}
}

func openUpdater(ctx context.Context, rt *updaterv1.Runtime) (*updaterfacade.Updater, error) {
	profile, err := clientProfile()
	if err != nil {
		return nil, err
	}
	opened := updaterfacade.Open(ctx, profile, rt, nil)
	if opened.Kind != "opened" || opened.Updater == nil {
		msg := "open sidecar failed"
		if opened.Error != nil && opened.Error.Message != "" {
			msg = opened.Error.Message
		}
		return nil, errors.New(msg)
	}
	return opened.Updater, nil
}

func checkAvailable(ctx context.Context, u *updaterfacade.Updater, force bool, exactCode int64) (*updaterv1.UpdateAvailable, error) {
	cr := u.Check(ctx, force, exactCode, nil)
	switch k := cr.Kind.(type) {
	case *updaterv1.CheckResult_UpdateAvailable:
		return k.UpdateAvailable, nil
	case *updaterv1.CheckResult_Failed:
		if k.Failed.GetError() != nil {
			return nil, errors.New(k.Failed.GetError().GetMessage())
		}
		return nil, errors.New("check failed")
	case *updaterv1.CheckResult_FallbackRequired:
		return nil, fmt.Errorf("需要手工更新: %s", k.FallbackRequired.GetManualUrl())
	case *updaterv1.CheckResult_Throttled:
		return nil, errors.New("检查更新被节流")
	default:
		return nil, nil
	}
}

func downloadAndApply(ctx context.Context, u *updaterfacade.Updater, planID string) error {
	dr := u.Download(ctx, planID, nil)
	if dr.GetFailed() != nil {
		err := dr.GetFailed().GetError()
		if err != nil {
			return errors.New(err.GetMessage())
		}
		return errors.New("download failed")
	}
	ar := u.Apply(ctx, planID, nil)
	if ar.GetFailed() != nil {
		err := ar.GetFailed().GetError()
		if err != nil {
			return errors.New(err.GetMessage())
		}
		return errors.New("apply failed")
	}
	return nil
}

func semverCodeOrZero(version string) int64 {
	code, err := sdk.SemverCode(version)
	if err != nil {
		return 0
	}
	return int64(code)
}

func binInstallRoot() (string, error) {
	if root, err := repo.GetRootDir(); err == nil {
		return filepath.Join(root, "bin"), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}
