package agenttools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/types"
)

func mustPayload(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage(`{}`)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func planFail(name, msg string) *PlanResult {
	return &PlanResult{Name: name, Error: msg}
}

// Plan 把 Agent 工具调用编成步骤表。Console 按步骤执行；本函数不碰网关。
func Plan(name string, arguments json.RawMessage) *PlanResult {
	name = strings.TrimSpace(name)
	def, err := lookupTool(name)
	if err != nil {
		return planFail(name, err.Error())
	}
	if def.Owner == OwnerConsole {
		return &PlanResult{Name: name, Owner: OwnerConsole, Shape: ShapeSingle, Steps: nil}
	}
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	switch name {
	case "dec_list_managed_projects":
		return singleInvoke(name, "list_managed_projects", "", "global", nil)
	case "dec_list_managed_devices":
		return singleInvoke(name, "list_managed_devices", "", "global", nil)
	case "dec_connect_repo":
		var in connectRepoParams
		if err := json.Unmarshal(arguments, &in); err != nil {
			return planFail(name, err.Error())
		}
		return singleInvoke(name, "connect_repo", "", "global", map[string]any{"RepoURL": in.RepoURL})
	case "dec_register_managed_project":
		var in registerManagedProjectParams
		if err := json.Unmarshal(arguments, &in); err != nil {
			return planFail(name, err.Error())
		}
		if strings.TrimSpace(in.Root) == "" {
			return planFail(name, "root 不能为空")
		}
		return singleInvoke(name, "register_managed_project", "", "global", map[string]any{
			"Root": in.Root, "Label": in.Label,
		})
	case "dec_create_local_asset":
		var in createLocalAssetParams
		if err := json.Unmarshal(arguments, &in); err != nil {
			return planFail(name, err.Error())
		}
		plane, err := parseSinglePlane(in.Plane)
		if err != nil {
			return planFail(name, err.Error())
		}
		root, err := resolveWorkspace(plane, in.ProjectRoot)
		if err != nil {
			return planFail(name, err.Error())
		}
		return singleInvoke(name, "create_local_asset", root, string(plane), map[string]any{
			"Project": in.Project, "Kind": in.Kind, "Name": in.Name,
			"Visibility": in.Visibility, "Plane": in.AssetPlane,
		})
	case "dec_set_requires":
		var in setRequiresParams
		if err := json.Unmarshal(arguments, &in); err != nil {
			return planFail(name, err.Error())
		}
		plane, err := parseSinglePlane(in.Plane)
		if err != nil {
			return planFail(name, err.Error())
		}
		root, err := resolveWorkspace(plane, in.ProjectRoot)
		if err != nil {
			return planFail(name, err.Error())
		}
		return singleInvoke(name, "set_requires", root, string(plane), map[string]any{"Requires": in.Requires})
	case "dec_list_subscription_candidates":
		var in listSubscriptionCandidatesParams
		if err := json.Unmarshal(arguments, &in); err != nil {
			return planFail(name, err.Error())
		}
		plane, err := parseSinglePlane(in.Plane)
		if err != nil {
			return planFail(name, err.Error())
		}
		root, err := resolveWorkspace(plane, in.ProjectRoot)
		if err != nil {
			return planFail(name, err.Error())
		}
		return singleInvoke(name, "list_subscription_candidates", root, string(plane), nil)
	case "dec_propose_upstream":
		var in proposeUpstreamParams
		if err := json.Unmarshal(arguments, &in); err != nil {
			return planFail(name, err.Error())
		}
		root, err := resolveWorkspace(app.WorkspaceLocal, in.ProjectRoot)
		if err != nil {
			return planFail(name, err.Error())
		}
		return singleInvoke(name, "propose_upstream", root, "local", map[string]any{
			"ProjectRoot": in.ProjectRoot, "OriginRepo": in.OriginRepo, "Asset": in.Asset,
			"Title": in.Title, "Body": in.Body, "Diff": in.Diff, "Mode": in.Mode, "Branch": in.Branch,
		})
	case "dec_status":
		return planStatus(arguments)
	case "dec_init_project":
		return planInitProject(arguments)
	case "dec_list_assets":
		return planPlanes(name, arguments, StepInvoke, "load_asset_selection", nil)
	case "dec_pull":
		return planPlanes(name, arguments, StepRun, "pull", nil)
	case "dec_push":
		return planPlanes(name, arguments, StepRun, "push", nil)
	case "dec_preview_push":
		return planPlanes(name, arguments, StepRun, "preview_push", nil)
	case "dec_list_secrets":
		var in listSecretsParams
		if err := json.Unmarshal(arguments, &in); err != nil {
			return planFail(name, err.Error())
		}
		includeRemote := true
		if in.IncludeRemote != nil {
			includeRemote = *in.IncludeRemote
		}
		return planPlanes(name, arguments, StepInvoke, "list_secrets", map[string]any{"IncludeRemote": includeRemote})
	case "dec_list_delete_candidates":
		return planPlanes(name, arguments, StepInvoke, "list_delete_candidates", map[string]any{"IncludeRemote": true})
	case "dec_delete":
		return planDelete(arguments)
	case "dec_provision_remote":
		return planProvisionRemote(arguments)
	default:
		return planFail(name, fmt.Sprintf("工具 %s 未实现 Plan", name))
	}
}

func singleInvoke(name, method, root, plane string, payload any) *PlanResult {
	return &PlanResult{
		Name:  name,
		Owner: OwnerServer,
		Shape: ShapeSingle,
		Steps: []Step{{
			Kind: StepInvoke, Method: method, ProjectRoot: root, Plane: plane,
			Payload: mustPayload(payload),
		}},
	}
}

func planStatus(arguments json.RawMessage) *PlanResult {
	var in statusParams
	if err := json.Unmarshal(arguments, &in); err != nil {
		return planFail("dec_status", err.Error())
	}
	plane, err := parseSinglePlane(in.Plane)
	if err != nil {
		return planFail("dec_status", err.Error())
	}
	root, err := resolveWorkspace(plane, in.ProjectRoot)
	if err != nil {
		return planFail("dec_status", err.Error())
	}
	return &PlanResult{
		Name:  "dec_status",
		Owner: OwnerServer,
		Shape: ShapeKeyed,
		Envelope: map[string]any{
			"plane": string(plane),
		},
		Steps: []Step{
			{
				Kind: StepInvoke, Method: "load_project_overview",
				ProjectRoot: root, Plane: string(plane),
				Payload: mustPayload(map[string]any{"IncludeVaultBundles": true}),
				Key:     "project",
			},
			{
				Kind: StepConsoleActiveOperation, ProjectRoot: root, Key: "active_operation", Optional: true,
			},
		},
	}
}

func planInitProject(arguments json.RawMessage) *PlanResult {
	var in initProjectParams
	if err := json.Unmarshal(arguments, &in); err != nil {
		return planFail("dec_init_project", err.Error())
	}
	root, err := resolveWorkspace(app.WorkspaceLocal, in.ProjectRoot)
	if err != nil {
		return planFail("dec_init_project", err.Error())
	}
	steps := []Step{{
		Kind: StepInvoke, Method: "prepare_project_config_init",
		ProjectRoot: root, Plane: "local", Payload: mustPayload(nil), Key: "init",
	}}
	if in.ApplyVaultProject {
		steps = append(steps, Step{
			Kind: StepInvoke, Method: "apply_vault_project",
			ProjectRoot: root, Plane: "local", Payload: mustPayload(nil), Key: "vault_apply",
		})
	}
	return &PlanResult{
		Name:  "dec_init_project",
		Owner: OwnerServer,
		Shape: ShapeKeyed,
		Steps: steps,
	}
}

func planPlanes(name string, arguments json.RawMessage, kind, method string, payload any) *PlanResult {
	var wrapper struct {
		ProjectRoot string `json:"project_root"`
		Plane       string `json:"plane"`
	}
	if err := json.Unmarshal(arguments, &wrapper); err != nil {
		return planFail(name, err.Error())
	}
	planes, err := parsePlanes(wrapper.Plane)
	if err != nil {
		return planFail(name, err.Error())
	}
	if len(planes) == 1 {
		root, err := resolveWorkspace(planes[0], wrapper.ProjectRoot)
		if err != nil {
			return planFail(name, err.Error())
		}
		return &PlanResult{
			Name:  name,
			Owner: OwnerServer,
			Shape: ShapeSingle,
			Steps: []Step{{
				Kind: kind, Method: method, ProjectRoot: root, Plane: string(planes[0]),
				Payload: mustPayload(payload),
			}},
		}
	}
	steps := make([]Step, 0, len(planes))
	for _, plane := range planes {
		root, err := resolveWorkspace(plane, wrapper.ProjectRoot)
		step := Step{
			Kind: kind, Method: method, Plane: string(plane),
			Payload: mustPayload(payload), Key: string(plane),
		}
		if err != nil {
			step.Kind = StepError
			step.Method = err.Error()
			step.Optional = true
		} else {
			step.ProjectRoot = root
		}
		steps = append(steps, step)
	}
	return &PlanResult{
		Name:  name,
		Owner: OwnerServer,
		Shape: ShapePlanes,
		Steps: steps,
	}
}

func planDelete(arguments json.RawMessage) *PlanResult {
	var in deleteParams
	if err := json.Unmarshal(arguments, &in); err != nil {
		return planFail("dec_delete", err.Error())
	}
	plane, err := parseSinglePlane(in.Plane)
	if err != nil {
		return planFail("dec_delete", err.Error())
	}
	root, err := resolveWorkspace(plane, in.ProjectRoot)
	if err != nil {
		return planFail("dec_delete", err.Error())
	}
	items := make([]map[string]any, 0, len(in.Items))
	for _, item := range in.Items {
		items = append(items, map[string]any{
			"Kind": item.Kind, "Type": item.Type, "Name": item.Name, "Vault": item.Vault,
			"SecretPath": item.SecretPath, "SecretsBundle": item.SecretsBundle,
			"BundleName": item.BundleName, "ProjectName": item.ProjectName,
			"Visibility": types.AssetVisibility(item.Visibility),
			"AssetPlane": types.AssetPlane(item.AssetPlane),
		})
	}
	return &PlanResult{
		Name:  "dec_delete",
		Owner: OwnerServer,
		Shape: ShapeSingle,
		Steps: []Step{{
			Kind: StepRun, Method: "delete", ProjectRoot: root, Plane: string(plane),
			Payload: mustPayload(map[string]any{"Items": items, "Confirmed": in.Confirmed}),
		}},
	}
}

func planProvisionRemote(arguments json.RawMessage) *PlanResult {
	var in provisionRemoteParams
	if err := json.Unmarshal(arguments, &in); err != nil {
		return planFail("dec_provision_remote", err.Error())
	}
	if strings.TrimSpace(in.SSHTarget) == "" {
		return planFail("dec_provision_remote", "ssh_target 不能为空")
	}
	root := app.DeviceOperationKey(app.RemoteTarget{Alias: strings.TrimSpace(in.SSHTarget)})
	return &PlanResult{
		Name:  "dec_provision_remote",
		Owner: OwnerServer,
		Shape: ShapeSingle,
		Steps: []Step{{
			Kind: StepRun, Method: "provision_remote_host", ProjectRoot: root, Plane: "global",
			Payload: mustPayload(map[string]any{
				"Alias":     in.Alias,
				"Target":    map[string]any{"Alias": strings.TrimSpace(in.SSHTarget)},
				"Tags":      in.Tags,
				"Branch":    in.Branch,
				"Confirmed": in.Confirmed,
			}),
		}},
	}
}
