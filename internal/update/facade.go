package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	updaterv1 "go.firoyang.com/relkit/api/updater/v1"
	"go.firoyang.com/relkit/sdk"
	"go.firoyang.com/relkit/sdk/updaterfacade"
)

func clientProfile() (*updaterv1.ClientProfile, error) {
	keys, err := protoTrustedKeys()
	if err != nil {
		return nil, err
	}
	return &updaterv1.ClientProfile{
		Product:         productName,
		AllowedChannels: []string{"dev", "stable"},
		EntryUrls:       entryURLs(),
		TrustedKeys:     keys,
		Recovery:        embeddedRecovery(),
	}, nil
}

func protoTrustedKeys() ([]*updaterv1.TrustedKey, error) {
	var cfg relkitFile
	if err := json.Unmarshal(embeddedRelkitJSON, &cfg); err != nil {
		return nil, fmt.Errorf("parse embedded relkit.json: %w", err)
	}
	if len(cfg.Signing.PublicKeys) == 0 {
		return nil, fmt.Errorf("embedded relkit.json has no signing.publicKeys")
	}
	out := make([]*updaterv1.TrustedKey, 0, len(cfg.Signing.PublicKeys))
	haveRequired := false
	for _, pk := range cfg.Signing.PublicKeys {
		if pk.KeyID == "" {
			return nil, fmt.Errorf("embedded public key missing keyId")
		}
		raw, err := base64.StdEncoding.DecodeString(pk.PublicKeyBase64)
		if err != nil {
			return nil, fmt.Errorf("decode public key %q: %w", pk.KeyID, err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("public key %q: invalid length %d", pk.KeyID, len(raw))
		}
		if pk.KeyID == keyID {
			haveRequired = true
		}
		out = append(out, &updaterv1.TrustedKey{KeyId: pk.KeyID, PublicKey: raw})
	}
	if !haveRequired {
		return nil, fmt.Errorf("embedded relkit.json missing required keyId %q", keyID)
	}
	return out, nil
}

func embeddedRecovery() *updaterv1.RecoveryHelp {
	var cfg struct {
		Recovery *struct {
			Message string `json:"message"`
			Links   []struct {
				Label string `json:"label"`
				URL   string `json:"url"`
			} `json:"links"`
		} `json:"recovery"`
	}
	if err := json.Unmarshal(embeddedRelkitJSON, &cfg); err != nil || cfg.Recovery == nil {
		return nil
	}
	help := &updaterv1.RecoveryHelp{Message: cfg.Recovery.Message}
	for _, link := range cfg.Recovery.Links {
		help.Links = append(help.Links, &updaterv1.RecoveryLink{Label: link.Label, Url: link.URL})
	}
	return help
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

func fileSetRuntime(component, goos, goarch string, currentCode int64, installRoot string) *updaterv1.Runtime {
	name := component
	if goos == "windows" {
		name += ".exe"
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
			Layout:      updaterv1.Layout_LAYOUT_FILE_SET,
			InstallRoot: installRoot,
			FileSet: []*updaterv1.FileSetEntry{{
				DestRelpath:  name,
				ArtifactName: name,
			}},
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
