package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/Mxucc/StorFlow/application/constants"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/ent/davaccount"
	"github.com/Mxucc/StorFlow/ent/group"
	"github.com/Mxucc/StorFlow/ent/metadata"
	"github.com/Mxucc/StorFlow/ent/node"
	"github.com/Mxucc/StorFlow/ent/oauthclient"
	"github.com/Mxucc/StorFlow/ent/setting"
	"github.com/Mxucc/StorFlow/ent/storagepolicy"
	"github.com/Mxucc/StorFlow/inventory/debug"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/boolset"
	"github.com/Mxucc/StorFlow/pkg/cache"
	"github.com/Mxucc/StorFlow/pkg/logging"
	"github.com/Mxucc/StorFlow/pkg/util"
	"github.com/samber/lo"
)

// needMigration exams if required schema version is satisfied.
func needMigration(client *ent.Client, ctx context.Context, requiredDbVersion string) bool {
	c, _ := client.Setting.Query().Where(setting.NameEQ(DBVersionPrefix + requiredDbVersion)).Count(ctx)
	return c == 0
}

func migrate(l logging.Logger, client *ent.Client, ctx context.Context, kv cache.Driver, requiredDbVersion string) error {
	l.Info("Start initializing database schema...")
	l.Info("Creating basic table schema...")
	if err := client.Schema.Create(ctx); err != nil {
		return fmt.Errorf("Failed creating schema resources: %w", err)
	}

	migrateDefaultSettings(l, client, ctx, kv)

	if err := migrateDefaultStoragePolicy(l, client, ctx); err != nil {
		return fmt.Errorf("failed migrating default storage policy: %w", err)
	}

	if err := migrateSysGroups(l, client, ctx); err != nil {
		return fmt.Errorf("failed migrating default storage policy: %w", err)
	}

	if err := migrateOAuthClient(l, client, ctx); err != nil {
		return fmt.Errorf("failed migrating OAuth client: %w", err)
	}

	if err := applyPatches(l, client, ctx, requiredDbVersion); err != nil {
		return fmt.Errorf("failed applying schema patches: %w", err)
	}

	client.Setting.Create().SetName(DBVersionPrefix + requiredDbVersion).SetValue("installed").Save(ctx)
	return nil
}

func migrateDefaultSettings(l logging.Logger, client *ent.Client, ctx context.Context, kv cache.Driver) {
	// clean kv cache
	if err := kv.DeleteAll(); err != nil {
		l.Warning("Failed to remove all KV entries while schema migration: %s", err)
	}

	// List existing settings into a map
	existingSettings := make(map[string]struct{})
	settings, err := client.Setting.Query().All(ctx)
	if err != nil {
		l.Warning("Failed to query existing settings: %s", err)
	}

	for _, s := range settings {
		existingSettings[s.Name] = struct{}{}
	}

	l.Info("Insert default settings...")
	for k, v := range DefaultSettings {
		if _, ok := existingSettings[k]; ok {
			l.Debug("Skip inserting setting %s, already exists.", k)
			continue
		}

		if override, ok := os.LookupEnv(EnvDefaultOverwritePrefix + k); ok {
			l.Info("Override default setting %q with env value %q", k, override)
			v = override
		}

		client.Setting.Create().SetName(k).SetValue(v).SaveX(ctx)
	}
}

// kvSettingCachePrefix must match setting.KvSettingPrefix in pkg/setting, which
// cannot be imported here without creating an import cycle.
const kvSettingCachePrefix = "setting_"

// defaultSettingSkipList lists default settings that must never be seeded
// implicitly: their default values are randomly generated, so inserting them
// after the fact would invalidate existing hashes or encrypted data.
var defaultSettingSkipList = map[string]struct{}{
	"secret_key":         {},
	"hash_id_salt":       {},
	"encrypt_master_key": {},
}

// ensureDefaultSettings inserts default settings missing from the database.
//
// Unlike migrateDefaultSettings it runs on every startup, so settings introduced
// by a new build (for example new mail templates) become available on upgraded
// installations that do not bump the database version marker. Existing values are
// never overwritten.
func ensureDefaultSettings(l logging.Logger, client *ent.Client, ctx context.Context, kv cache.Driver) {
	settings, err := client.Setting.Query().All(ctx)
	if err != nil {
		l.Warning("Failed to query existing settings: %s", err)
		return
	}

	existingSettings := make(map[string]struct{}, len(settings))
	for _, s := range settings {
		existingSettings[s.Name] = struct{}{}
	}

	added := make([]string, 0)
	for k, v := range DefaultSettings {
		if _, ok := existingSettings[k]; ok {
			continue
		}

		if _, skip := defaultSettingSkipList[k]; skip {
			l.Debug("Skip seeding setting %q, its default value is generated at install time.", k)
			continue
		}

		if override, ok := os.LookupEnv(EnvDefaultOverwritePrefix + k); ok {
			l.Info("Override default setting %q with env value %q", k, override)
			v = override
		}

		if err := client.Setting.Create().SetName(k).SetValue(v).Exec(ctx); err != nil {
			l.Warning("Failed to insert default setting %q: %s", k, err)
			continue
		}

		added = append(added, k)
	}

	if len(added) == 0 {
		return
	}

	// Drop cached values (including cached misses) of the inserted settings so
	// that the new value is picked up without a restart.
	if kv != nil {
		if err := kv.Delete(kvSettingCachePrefix, added...); err != nil {
			l.Warning("Failed to clear cached setting values: %s", err)
		}
	}

	l.Info("Inserted %d missing default setting(s): %v", len(added), added)
}

func migrateDefaultStoragePolicy(l logging.Logger, client *ent.Client, ctx context.Context) error {
	if _, err := client.StoragePolicy.Query().Where(storagepolicy.ID(1)).First(ctx); err == nil {
		l.Info("Default storage policy (ID=1) already exists, skip migrating.")
		return nil
	}

	l.Info("Insert default storage policy...")
	if _, err := client.StoragePolicy.Create().
		SetName("Default storage policy").
		SetType(types.PolicyTypeLocal).
		SetDirNameRule(util.DataPath("uploads/{uid}/{path}")).
		SetFileNameRule("{uid}_{randomkey8}_{originname}").
		SetSettings(&types.PolicySetting{
			ChunkSize:   25 << 20, // 25MB
			PreAllocate: true,
		}).
		Save(ctx); err != nil {
		return fmt.Errorf("failed to create default storage policy: %w", err)
	}

	return nil
}

func migrateSysGroups(l logging.Logger, client *ent.Client, ctx context.Context) error {
	if err := migrateAdminGroup(l, client, ctx); err != nil {
		return err
	}

	if err := migrateUserGroup(l, client, ctx); err != nil {
		return err
	}

	if err := migrateAnonymousGroup(l, client, ctx); err != nil {
		return err
	}

	if err := migrateMasterNode(l, client, ctx); err != nil {
		return err
	}

	return nil
}

func migrateAdminGroup(l logging.Logger, client *ent.Client, ctx context.Context) error {
	if _, err := client.Group.Query().Where(group.ID(1)).First(ctx); err == nil {
		l.Info("Default admin group (ID=1) already exists, skip migrating.")
		return nil
	}

	l.Info("Insert default admin group...")
	permissions := &boolset.BooleanSet{}
	boolset.Sets(map[types.GroupPermission]bool{
		types.GroupPermissionIsAdmin:             true,
		types.GroupPermissionShare:               true,
		types.GroupPermissionWebDAV:              true,
		types.GroupPermissionWebDAVProxy:         true,
		types.GroupPermissionArchiveDownload:     true,
		types.GroupPermissionArchiveTask:         true,
		types.GroupPermissionShareDownload:       true,
		types.GroupPermissionRemoteDownload:      true,
		types.GroupPermissionRedirectedSource:    true,
		types.GroupPermissionAdvanceDelete:       true,
		types.GroupPermissionIgnoreFileOwnership: true,
		// TODO: review default permission
	}, permissions)
	if _, err := client.Group.Create().
		SetName("Admin").
		SetStoragePoliciesID(1).
		SetMaxStorage(1 * constants.TB). // 1 TB default storage
		SetPermissions(permissions).
		SetSettings(&types.GroupSetting{
			SourceBatchSize:  1000,
			Aria2BatchSize:   50,
			MaxWalkedFiles:   100000,
			TrashRetention:   7 * 24 * 3600,
			RedirectedSource: true,
		}).
		Save(ctx); err != nil {
		return fmt.Errorf("failed to create default admin group: %w", err)
	}

	return nil
}

func migrateUserGroup(l logging.Logger, client *ent.Client, ctx context.Context) error {
	if _, err := client.Group.Query().Where(group.ID(2)).First(ctx); err == nil {
		l.Info("Default user group (ID=2) already exists, skip migrating.")
		return nil
	}

	l.Info("Insert default user group...")
	permissions := &boolset.BooleanSet{}
	boolset.Sets(map[types.GroupPermission]bool{
		types.GroupPermissionShare:            true,
		types.GroupPermissionShareDownload:    true,
		types.GroupPermissionRedirectedSource: true,
	}, permissions)
	if _, err := client.Group.Create().
		SetName("User").
		SetStoragePoliciesID(1).
		SetMaxStorage(1 * constants.GB). // 1 GB default storage
		SetPermissions(permissions).
		SetSettings(&types.GroupSetting{
			SourceBatchSize:  10,
			Aria2BatchSize:   1,
			MaxWalkedFiles:   100000,
			TrashRetention:   7 * 24 * 3600,
			RedirectedSource: true,
		}).
		Save(ctx); err != nil {
		return fmt.Errorf("failed to create default user group: %w", err)
	}

	return nil
}

func migrateAnonymousGroup(l logging.Logger, client *ent.Client, ctx context.Context) error {
	if _, err := client.Group.Query().Where(group.ID(AnonymousGroupID)).First(ctx); err == nil {
		l.Info("Default anonymous group (ID=3) already exists, skip migrating.")
		return nil
	}

	l.Info("Insert default anonymous group...")
	permissions := &boolset.BooleanSet{}
	boolset.Sets(map[types.GroupPermission]bool{
		types.GroupPermissionIsAnonymous:   true,
		types.GroupPermissionShareDownload: true,
	}, permissions)
	if _, err := client.Group.Create().
		SetName("Anonymous").
		SetPermissions(permissions).
		SetSettings(&types.GroupSetting{
			MaxWalkedFiles:   100000,
			RedirectedSource: true,
		}).
		Save(ctx); err != nil {
		return fmt.Errorf("failed to create default anonymous group: %w", err)
	}

	return nil
}

func migrateMasterNode(l logging.Logger, client *ent.Client, ctx context.Context) error {
	if _, err := client.Node.Query().Where(node.TypeEQ(node.TypeMaster)).First(ctx); err == nil {
		l.Info("Default master node already exists, skip migrating.")
		return nil
	}

	capabilities := &boolset.BooleanSet{}
	boolset.Sets(map[types.NodeCapability]bool{
		types.NodeCapabilityCreateArchive:  true,
		types.NodeCapabilityExtractArchive: true,
		types.NodeCapabilityRemoteDownload: true,
	}, capabilities)

	stm := client.Node.Create().
		SetType(node.TypeMaster).
		SetCapabilities(capabilities).
		SetName("Master").
		SetSettings(&types.NodeSetting{
			Provider: types.DownloaderProviderAria2,
		}).
		SetStatus(node.StatusActive)

	_, enableAria2 := os.LookupEnv(EnvEnableAria2)
	if enableAria2 {
		l.Info("Aria2 is override as enabled.")
		stm.SetSettings(&types.NodeSetting{
			Provider: types.DownloaderProviderAria2,
			Aria2Setting: &types.Aria2Setting{
				Server: "http://127.0.0.1:6800/jsonrpc",
			},
		})
	}

	l.Info("Insert default master node...")
	if _, err := stm.Save(ctx); err != nil {
		return fmt.Errorf("failed to create default master node: %w", err)
	}

	return nil
}

const (
	OAuthClientDesktopGUID        = "393a1839-f52e-498e-9972-e77cc2241eee"
	OAuthClientDesktopSecret      = "8GaQIu3lOSdqYoDHi9cR8IZ4pvuMH8ya"
	OAuthClientDesktopName        = "application:oauth.desktop"
	OAuthClientDesktopRedirectURI = "/callback/desktop"
	OAuthClientCLIGUID            = "6326d2af-2fef-4a99-94da-1ee8ef0ca53f"
	OAuthClientCLISecret          = "yoFiNgbxvSCzK2Nm92T3TNaREh4qBjq4"
	OAuthClientCLIName            = "StorFlow CLI"
	OAuthClientCLIRedirectURI     = "http://127.0.0.1/callback"
	OAuthClientiOSGUID            = "220db97a-44a3-44f7-99b6-d767262b4daa"
	OAuthClientiOSSecret          = "1kxOW4IyVOkPlsKCnTwzfHyP8XrbpfaF"
	OAuthClientiOSName            = "application:setting.iOSApp"
	OAuthClientiOSRedirectURI     = "/callback/ios"
)

func migrateOAuthClient(l logging.Logger, client *ent.Client, ctx context.Context) error {
	if err := migrateOAuthClientDesktop(l, client, ctx); err != nil {
		return err
	}

	if err := migrateOAuthClientiOS(l, client, ctx); err != nil {
		return err
	}

	if err := migrateOAuthClientCLI(l, client, ctx); err != nil {
		return err
	}

	return nil
}

func migrateOAuthClientiOS(l logging.Logger, client *ent.Client, ctx context.Context) error {
	if _, err := client.OAuthClient.Query().Where(oauthclient.GUID(OAuthClientiOSGUID)).First(ctx); err == nil {
		l.Info("Default OAuth client (GUID=%s) already exists, skip migrating.", OAuthClientiOSGUID)
		return nil
	}
	if _, err := client.OAuthClient.Create().
		SetGUID(OAuthClientiOSGUID).
		SetSecret(OAuthClientiOSSecret).
		SetName(OAuthClientiOSName).
		SetRedirectUris([]string{OAuthClientiOSRedirectURI}).
		SetScopes([]string{"profile", "email", "openid", "offline_access", "UserInfo.Write", "UserSecurityInfo.Write", "Workflow.Write", "Files.Write", "Shares.Write", "Finance.Write", "DavAccount.Write"}).
		SetProps(&types.OAuthClientProps{Icon: "/static/img/brand/icon.svg", RefreshTokenTTL: 7776000}).
		SetIsEnabled(true).
		Save(ctx); err != nil {
		return fmt.Errorf("failed to create default OAuth client: %w", err)
	}

	return nil
}

func migrateOAuthClientDesktop(l logging.Logger, client *ent.Client, ctx context.Context) error {
	if _, err := client.OAuthClient.Query().Where(oauthclient.GUID(OAuthClientDesktopGUID)).First(ctx); err == nil {
		l.Info("Default OAuth client (GUID=%s) already exists, skip migrating.", OAuthClientDesktopGUID)
		return nil
	}

	if _, err := client.OAuthClient.Create().
		SetGUID(OAuthClientDesktopGUID).
		SetSecret(OAuthClientDesktopSecret).
		SetName(OAuthClientDesktopName).
		SetRedirectUris([]string{OAuthClientDesktopRedirectURI}).
		SetScopes([]string{"profile", "email", "openid", "offline_access", "UserInfo.Write", "Workflow.Write", "Files.Write", "Shares.Write"}).
		SetProps(&types.OAuthClientProps{Icon: "/static/img/brand/icon.svg", RefreshTokenTTL: 7776000}).
		SetIsEnabled(true).
		Save(ctx); err != nil {
		return fmt.Errorf("failed to create default OAuth client: %w", err)
	}

	return nil
}

// migrateOAuthClientCLI preserves administrator changes to an existing built-in client.
func migrateOAuthClientCLI(l logging.Logger, client *ent.Client, ctx context.Context) error {
	if _, err := client.OAuthClient.Query().Where(oauthclient.GUID(OAuthClientCLIGUID)).First(ctx); err == nil {
		l.Info("Default OAuth client (GUID=%s) already exists, skip migrating.", OAuthClientCLIGUID)
		return nil
	} else if !ent.IsNotFound(err) {
		return fmt.Errorf("failed to query default CLI OAuth client: %w", err)
	}

	if _, err := client.OAuthClient.Create().
		SetGUID(OAuthClientCLIGUID).
		SetSecret(OAuthClientCLISecret).
		SetName(OAuthClientCLIName).
		SetRedirectUris([]string{OAuthClientCLIRedirectURI}).
		SetScopes([]string{"profile", "email", "openid", "offline_access", "UserInfo.Write", "Workflow.Write", "Files.Write", "Shares.Write"}).
		SetProps(&types.OAuthClientProps{Icon: "/static/img/brand/icon.svg", RefreshTokenTTL: 7776000}).
		SetIsEnabled(true).
		Save(ctx); err != nil {
		return fmt.Errorf("failed to create default CLI OAuth client: %w", err)
	}
	return nil
}

type (
	PatchFunc func(l logging.Logger, client *ent.Client, ctx context.Context) error
	Patch     struct {
		Name       string
		EndVersion string
		Func       PatchFunc
	}
)

var patches = []Patch{
	{
		Name:       "apply_default_archive_viewer",
		EndVersion: "4.7.0",
		Func: func(l logging.Logger, client *ent.Client, ctx context.Context) error {
			fileViewersSetting, err := client.Setting.Query().Where(setting.Name("file_viewers")).First(ctx)
			if err != nil {
				return fmt.Errorf("failed to query file_viewers setting: %w", err)
			}

			var fileViewers []types.ViewerGroup
			if err := json.Unmarshal([]byte(fileViewersSetting.Value), &fileViewers); err != nil {
				return fmt.Errorf("failed to unmarshal file_viewers setting: %w", err)
			}

			fileViewerExisted := false
			for _, viewer := range fileViewers[0].Viewers {
				if viewer.ID == "archive" {
					fileViewerExisted = true
					break
				}
			}

			// 2.2 If not existed, add it
			if !fileViewerExisted {
				// Found existing archive viewer default setting
				var defaultArchiveViewer types.Viewer
				for _, viewer := range defaultFileViewers[0].Viewers {
					if viewer.ID == "archive" {
						defaultArchiveViewer = viewer
						break
					}
				}

				fileViewers[0].Viewers = append(fileViewers[0].Viewers, defaultArchiveViewer)
				newFileViewersSetting, err := json.Marshal(fileViewers)
				if err != nil {
					return fmt.Errorf("failed to marshal file_viewers setting: %w", err)
				}

				if _, err := client.Setting.UpdateOne(fileViewersSetting).SetValue(string(newFileViewersSetting)).Save(ctx); err != nil {
					return fmt.Errorf("failed to update file_viewers setting: %w", err)
				}
			}

			return nil
		},
	},
	{
		Name:       "apply_default_excalidraw_viewer",
		EndVersion: "4.1.0",
		Func: func(l logging.Logger, client *ent.Client, ctx context.Context) error {
			// 1. Apply excalidraw file icons
			// 1.1 Check if it's already applied
			iconSetting, err := client.Setting.Query().Where(setting.Name("explorer_icons")).First(ctx)
			if err != nil {
				return fmt.Errorf("failed to query explorer_icons setting: %w", err)
			}

			var icons []types.FileTypeIconSetting
			if err := json.Unmarshal([]byte(iconSetting.Value), &icons); err != nil {
				return fmt.Errorf("failed to unmarshal explorer_icons setting: %w", err)
			}

			iconExisted := false
			for _, icon := range icons {
				if lo.Contains(icon.Exts, "excalidraw") {
					iconExisted = true
					break
				}
			}

			// 1.2 If not existed, add it
			if !iconExisted {
				// Found existing excalidraw icon default setting
				var defaultExcalidrawIcon types.FileTypeIconSetting
				for _, icon := range defaultIcons {
					if lo.Contains(icon.Exts, "excalidraw") {
						defaultExcalidrawIcon = icon
						break
					}
				}

				icons = append(icons, defaultExcalidrawIcon)
				newIconSetting, err := json.Marshal(icons)
				if err != nil {
					return fmt.Errorf("failed to marshal explorer_icons setting: %w", err)
				}

				if _, err := client.Setting.UpdateOne(iconSetting).SetValue(string(newIconSetting)).Save(ctx); err != nil {
					return fmt.Errorf("failed to update explorer_icons setting: %w", err)
				}
			}

			// 2. Apply default file viewers
			// 2.1 Check if it's already applied
			fileViewersSetting, err := client.Setting.Query().Where(setting.Name("file_viewers")).First(ctx)
			if err != nil {
				return fmt.Errorf("failed to query file_viewers setting: %w", err)
			}

			var fileViewers []types.ViewerGroup
			if err := json.Unmarshal([]byte(fileViewersSetting.Value), &fileViewers); err != nil {
				return fmt.Errorf("failed to unmarshal file_viewers setting: %w", err)
			}

			fileViewerExisted := false
			for _, viewer := range fileViewers[0].Viewers {
				if viewer.ID == "excalidraw" {
					fileViewerExisted = true
					break
				}
			}

			// 2.2 If not existed, add it
			if !fileViewerExisted {
				// Found existing excalidraw viewer default setting
				var defaultExcalidrawViewer types.Viewer
				for _, viewer := range defaultFileViewers[0].Viewers {
					if viewer.ID == "excalidraw" {
						defaultExcalidrawViewer = viewer
						break
					}
				}

				fileViewers[0].Viewers = append(fileViewers[0].Viewers, defaultExcalidrawViewer)
				newFileViewersSetting, err := json.Marshal(fileViewers)
				if err != nil {
					return fmt.Errorf("failed to marshal file_viewers setting: %w", err)
				}

				if _, err := client.Setting.UpdateOne(fileViewersSetting).SetValue(string(newFileViewersSetting)).Save(ctx); err != nil {
					return fmt.Errorf("failed to update file_viewers setting: %w", err)
				}
			}

			return nil
		},
	},
	{
		Name:       "apply_email_title_magic_var",
		EndVersion: "4.7.0",
		Func: func(l logging.Logger, client *ent.Client, ctx context.Context) error {
			// 1. Activate Template
			mailActivationTemplateSetting, err := client.Setting.Query().Where(setting.Name("mail_activation_template")).First(ctx)
			if err != nil {
				return fmt.Errorf("failed to query mail_activation_template setting: %w", err)
			}

			var mailActivationTemplate []struct {
				Title    string `json:"title"`
				Body     string `json:"body"`
				Language string `json:"language"`
			}
			if err := json.Unmarshal([]byte(mailActivationTemplateSetting.Value), &mailActivationTemplate); err != nil {
				return fmt.Errorf("failed to unmarshal mail_activation_template setting: %w", err)
			}

			for i, t := range mailActivationTemplate {
				mailActivationTemplate[i].Title = fmt.Sprintf("[{{ .CommonContext.SiteBasic.Name }}] %s", t.Title)
			}

			newMailActivationTemplate, err := json.Marshal(mailActivationTemplate)
			if err != nil {
				return fmt.Errorf("failed to marshal mail_activation_template setting: %w", err)
			}

			if _, err := client.Setting.UpdateOne(mailActivationTemplateSetting).SetValue(string(newMailActivationTemplate)).Save(ctx); err != nil {
				return fmt.Errorf("failed to update mail_activation_template setting: %w", err)
			}

			// 2. Reset Password Template
			mailResetTemplateSetting, err := client.Setting.Query().Where(setting.Name("mail_reset_template")).First(ctx)
			if err != nil {
				return fmt.Errorf("failed to query mail_reset_template setting: %w", err)
			}

			var mailResetTemplate []struct {
				Title    string `json:"title"`
				Body     string `json:"body"`
				Language string `json:"language"`
			}
			if err := json.Unmarshal([]byte(mailResetTemplateSetting.Value), &mailResetTemplate); err != nil {
				return fmt.Errorf("failed to unmarshal mail_reset_template setting: %w", err)
			}

			for i, t := range mailResetTemplate {
				mailResetTemplate[i].Title = fmt.Sprintf("[{{ .CommonContext.SiteBasic.Name }}] %s", t.Title)
			}

			newMailResetTemplate, err := json.Marshal(mailResetTemplate)
			if err != nil {
				return fmt.Errorf("failed to marshal mail_reset_template setting: %w", err)
			}

			if _, err := client.Setting.UpdateOne(mailResetTemplateSetting).SetValue(string(newMailResetTemplate)).Save(ctx); err != nil {
				return fmt.Errorf("failed to update mail_reset_template setting: %w", err)
			}

			return nil
		},
	},
	{
		Name:       "apply_thumb_path_magic_var",
		EndVersion: "4.10.0",
		Func: func(l logging.Logger, client *ent.Client, ctx context.Context) error {
			thumbSuffixSetting, err := client.Setting.Query().Where(setting.Name("thumb_entity_suffix")).First(ctx)
			if err != nil {
				return fmt.Errorf("failed to query thumb_entity_suffix setting: %w", err)
			}

			newThumbSuffix := fmt.Sprintf("{blob_path}/{blob_name}%s", thumbSuffixSetting.Value)
			if _, err := client.Setting.UpdateOne(thumbSuffixSetting).SetValue(newThumbSuffix).Save(ctx); err != nil {
				return fmt.Errorf("failed to update thumb_entity_suffix setting: %w", err)
			}

			return nil
		},
	},
	{
		Name:       "reset_secret_key",
		EndVersion: "4.13.0",
		Func: func(l logging.Logger, client *ent.Client, ctx context.Context) error {
			newSecretKey := util.RandStringRunesCrypto(256)
			ctx = context.WithValue(ctx, debug.SkipDbLogging{}, true)
			if err := client.Setting.Update().Where(setting.Name("secret_key")).SetValue(newSecretKey).Exec(ctx); err != nil {
				return fmt.Errorf("failed to update secret_key setting: %w", err)
			}

			return nil
		},
	},
}

func applyPatches(l logging.Logger, client *ent.Client, ctx context.Context, requiredDbVersion string) error {
	allVersionMarks, err := client.Setting.Query().Where(setting.NameHasPrefix(DBVersionPrefix)).All(ctx)
	if err != nil {
		return err
	}

	requiredDbVersion = strings.TrimSuffix(requiredDbVersion, "-pro")

	// Find the latest applied version
	var latestAppliedVersion *semver.Version
	for _, v := range allVersionMarks {
		v.Name = strings.TrimSuffix(v.Name, "-pro")
		version, err := semver.NewVersion(strings.TrimPrefix(v.Name, DBVersionPrefix))
		if err != nil {
			l.Warning("Failed to parse past version %s: %s", v.Name, err)
			continue
		}
		if latestAppliedVersion == nil || version.Compare(latestAppliedVersion) > 0 {
			latestAppliedVersion = version
		}
	}

	requiredVersion, err := semver.NewVersion(requiredDbVersion)
	if err != nil {
		return fmt.Errorf("failed to parse required version %s: %w", requiredDbVersion, err)
	}

	if latestAppliedVersion == nil || requiredVersion.Compare(requiredVersion) > 0 {
		latestAppliedVersion = requiredVersion
	}

	for _, patch := range patches {
		if latestAppliedVersion.Compare(semver.MustParse(patch.EndVersion)) < 0 {
			l.Info("Applying schema patch %s...", patch.Name)
			if err := patch.Func(l, client, ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// 品牌升级：把老版本留下的旧品牌痕迹（设置值、主题、图标、URI scheme）改成 StorFlow
// ---------------------------------------------------------------------------

const (
	// storflowThemeID 是内置 StorFlow 主题在 theme_options 里的 id（DeepSeek 蓝）。
	storflowThemeID = "#4D6BFE"
	// storflowInkThemeID 是内置的单色主题 id。
	storflowInkThemeID = "#111827"
	// legacyCLIClientName / storflowCLIClientName 是内置 CLI OAuth 客户端的旧名与新名。
	legacyCLIClientName   = "Cloudreve CLI"
	storflowCLIClientName = "StorFlow CLI"
)

// storflowThemeDefinition 是内置 StorFlow 蓝主题（只覆盖主色，其余色板/字体/组件质感
// 由前端的设计系统决定）。
const storflowThemeDefinition = `{"light":{"palette":{"primary":{"main":"#4D6BFE","light":"#7C90FF","dark":"#2B45C9","contrastText":"#FFFFFF"},"secondary":{"main":"#6366F1","light":"#818CF8","dark":"#4338CA","contrastText":"#FFFFFF"}}},"dark":{"palette":{"primary":{"main":"#7C90FF","light":"#A5B4FF","dark":"#4D6BFE","contrastText":"#0A0F1F"},"secondary":{"main":"#A5B4FC","light":"#C7D2FE","dark":"#818CF8","contrastText":"#0A0F1F"}}}}`

// storflowInkThemeDefinition 是内置的单色（石墨）主题，作为蓝主题之外的备选。
const storflowInkThemeDefinition = `{"light":{"palette":{"primary":{"main":"#111827","light":"#374151","dark":"#030712","contrastText":"#FFFFFF"},"secondary":{"main":"#4D6BFE","light":"#7C90FF","dark":"#2B45C9","contrastText":"#FFFFFF"}}},"dark":{"palette":{"primary":{"main":"#9CA3AF","light":"#D1D5DB","dark":"#6B7280","contrastText":"#0B0E14"},"secondary":{"main":"#7C90FF","light":"#A5B4FF","dark":"#4D6BFE","contrastText":"#0A0F1F"}}}}`

// brandingSettingUpgrades 只在设置值仍等于旧版本默认值时才替换，避免覆盖管理员的修改。
// upstream 字段就是需要被清理掉的历史值。
var brandingSettingUpgrades = map[string]struct{ legacy, storflow string }{
	"fromName":           {legacy: "Cloudreve", storflow: "StorFlow"},
	"tos_url":            {legacy: "https://cloudreve.org/privacy-policy", storflow: "https://github.com/Mxucc/StorFlow"},
	"privacy_policy_url": {legacy: "https://cloudreve.org/privacy-policy", storflow: "https://github.com/Mxucc/StorFlow"},
	// 上游默认把 SMTP 指向自家服务，这里清空，交给管理员填自己的邮件服务。
	"smtpHost": {legacy: "smtp.cloudreve.com", storflow: ""},
	"smtpUser": {legacy: "smtp.cloudreve.com", storflow: ""},
}

// legacyThemeIDs 是历史内置主题 id；默认主题仍是其中之一时切到 StorFlow 主题。
var legacyThemeIDs = map[string]struct{}{
	"#1976d2": {},
	"#3f51b5": {},
	// 早期 StorFlow 构建使用过的青绿主题，已换成 DeepSeek 蓝。
	"#0E7490": {},
}

// retiredThemeIDs 是历史内置主题 id，从主题列表里移除（管理员自定义的主题不受影响）。
var retiredThemeIDs = []string{"#0E7490", "#0B1220"}

// brandingPathUpgrades 把内置品牌资源的旧路径换成 /static/img/brand/ 下的新路径。
//
// 新路径同时解决了两件事：改名后浏览器/Service Worker 一定重新拉取（不会继续用
// 缓存的旧 Logo），也让所有品牌图收敛到同一个目录。仅在值仍是旧内置路径时改写。
var brandingPathUpgrades = map[string]struct{ legacy, brand string }{
	"site_logo":       {legacy: "/static/img/logo.svg", brand: "/static/img/brand/logo.svg"},
	"site_logo_light": {legacy: "/static/img/logo_light.svg", brand: "/static/img/brand/logo-light.svg"},
	"pwa_small_icon":  {legacy: "/static/img/favicon.ico", brand: "/static/img/brand/favicon.ico"},
	"pwa_medium_icon": {legacy: "/static/img/logo192.png", brand: "/static/img/brand/icon-192.png"},
	"pwa_large_icon":  {legacy: "/static/img/logo512.png", brand: "/static/img/brand/icon-512.png"},
}

// legacyIconPaths 把旧版本内置的 OAuth 客户端图标换成 StorFlow 图标。
var legacyIconPaths = map[string]string{
	"/static/img/cloudreve.svg":     "/static/img/brand/icon.svg",
	"/static/img/cloudreve_ios.svg": "/static/img/brand/icon.svg",
}

// upgradeBrandingDefaults 把「还带着旧品牌痕迹」的安装改成 StorFlow：
// 邮件发件人名称、法务链接、默认主题，以及内置 OAuth 客户端的名称与图标。
//
// 每次启动都会执行，但只在当前值等于旧版本默认值时改写，因此不会覆盖管理员的设置。
func upgradeBrandingDefaults(l logging.Logger, client *ent.Client, ctx context.Context, kv cache.Driver) {
	settings, err := client.Setting.Query().All(ctx)
	if err != nil {
		l.Warning("Failed to query settings for branding upgrade: %s", err)
		return
	}

	current := make(map[string]string, len(settings))
	for _, s := range settings {
		current[s.Name] = s.Value
	}

	updates := make(map[string]string)

	for name, upgrade := range brandingSettingUpgrades {
		if value, ok := current[name]; ok && value == upgrade.legacy {
			updates[name] = upgrade.storflow
		}
	}

	if value, ok := current["defaultTheme"]; ok {
		if _, isUpstream := legacyThemeIDs[value]; isUpstream {
			updates["defaultTheme"] = storflowThemeID
		}
	}

	if value, ok := current["theme_options"]; ok {
		if merged, changed := ensureStorflowThemes(value); changed {
			updates["theme_options"] = merged
		}
	}

	for name, upgrade := range brandingPathUpgrades {
		if value, ok := current[name]; ok && value == upgrade.legacy {
			updates[name] = upgrade.brand
		}
	}

	upgradeOAuthClientBranding(l, client, ctx)
	upgradeLegacyURIValues(l, client, ctx)

	if len(updates) == 0 {
		return
	}

	upgraded := make([]string, 0, len(updates))
	for name, value := range updates {
		if err := client.Setting.Update().Where(setting.NameEQ(name)).SetValue(value).Exec(ctx); err != nil {
			l.Warning("Failed to upgrade branding setting %q: %s", name, err)
			continue
		}
		upgraded = append(upgraded, name)
	}

	if len(upgraded) == 0 {
		return
	}

	// 丢掉被改写设置的缓存（含缓存未命中记录），无需重启即可生效。
	if kv != nil {
		if err := kv.Delete(kvSettingCachePrefix, upgraded...); err != nil {
			l.Warning("Failed to clear cached setting values: %s", err)
		}
	}

	l.Info("Upgraded branding settings to StorFlow: %v", upgraded)
}

// ensureStorflowThemes 在主题列表里补上内置的 StorFlow 主题、移除已下线的旧主题，
// 管理员自定义的主题原样保留。
func ensureStorflowThemes(raw string) (string, bool) {
	var themes map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &themes); err != nil {
		return raw, false
	}

	changed := false
	for _, id := range retiredThemeIDs {
		if _, ok := themes[id]; ok {
			delete(themes, id)
			changed = true
		}
	}

	builtin := map[string]string{
		storflowThemeID:    storflowThemeDefinition,
		storflowInkThemeID: storflowInkThemeDefinition,
	}
	for id, definition := range builtin {
		if _, ok := themes[id]; ok {
			continue
		}
		themes[id] = json.RawMessage(definition)
		changed = true
	}

	if !changed {
		return raw, false
	}

	merged, err := json.Marshal(themes)
	if err != nil {
		return raw, false
	}

	return string(merged), true
}

// legacyURIScheme 是旧版本写入数据库的 URI scheme（只在迁移里出现，解析器已不再接受）。
const legacyURIScheme = "cloudreve://"

// legacyURIMarker 记录旧 scheme 数据是否已经改写完毕，避免每次启动都全量扫表。
// 迁移覆盖的表每增加一处就换一个版本号，保证升级安装也会补跑一次。
const legacyURIMarker = "branding_uri_scheme_upgraded_v2"

// upgradeLegacyURIValues 把数据库里残留的旧 scheme URI（分享跳转元数据、WebDAV 账号、
// 用户置顶项、任务参数、审计日志）统一改写成 StorFlowScheme，
// 确保解析器只认新 scheme 也不会丢数据。
func upgradeLegacyURIValues(l logging.Logger, client *ent.Client, ctx context.Context) {
	if row, err := client.Setting.Query().Where(setting.NameEQ(legacyURIMarker)).Only(ctx); err == nil && row.Value == "1" {
		return
	}

	newPrefix := constants.StorFlowScheme + "://"
	rewritten := 0

	// 文件元数据，例如 sys:shared_redirect 里存的分享 URI。
	metas, err := client.Metadata.Query().Where(metadata.ValueContains(legacyURIScheme)).All(ctx)
	if err != nil {
		l.Warning("Failed to query metadata for URI scheme upgrade: %s", err)
	}
	for _, meta := range metas {
		value := strings.ReplaceAll(meta.Value, legacyURIScheme, newPrefix)
		if err := client.Metadata.UpdateOneID(meta.ID).SetValue(value).Exec(ctx); err != nil {
			l.Warning("Failed to upgrade metadata %d URI scheme: %s", meta.ID, err)
			continue
		}
		rewritten++
	}

	// WebDAV 账号的根目录 URI。
	accounts, err := client.DavAccount.Query().Where(davaccount.URIHasPrefix(legacyURIScheme)).All(ctx)
	if err != nil {
		l.Warning("Failed to query WebDAV accounts for URI scheme upgrade: %s", err)
	}
	for _, account := range accounts {
		uri := newPrefix + strings.TrimPrefix(account.URI, legacyURIScheme)
		if err := client.DavAccount.UpdateOneID(account.ID).SetURI(uri).Exec(ctx); err != nil {
			l.Warning("Failed to upgrade WebDAV account %d URI scheme: %s", account.ID, err)
			continue
		}
		rewritten++
	}

	// 用户置顶项里保存的 URI。
	users, err := client.User.Query().All(ctx)
	if err != nil {
		l.Warning("Failed to query users for URI scheme upgrade: %s", err)
	}
	for _, u := range users {
		if u.Settings == nil || len(u.Settings.Pined) == 0 {
			continue
		}
		changed := false
		pins := make([]types.PinedFile, 0, len(u.Settings.Pined))
		for _, pin := range u.Settings.Pined {
			if strings.HasPrefix(pin.Uri, legacyURIScheme) {
				pin.Uri = newPrefix + strings.TrimPrefix(pin.Uri, legacyURIScheme)
				changed = true
			}
			pins = append(pins, pin)
		}
		if !changed {
			continue
		}
		settings := *u.Settings
		settings.Pined = pins
		if err := client.User.UpdateOneID(u.ID).SetSettings(&settings).Exec(ctx); err != nil {
			l.Warning("Failed to upgrade user %d pinned URIs: %s", u.ID, err)
			continue
		}
		rewritten++
	}

	// 任务参数（转存 / 解压 / 下载等）里保存的源、目标 URI。
	tasks, err := client.Task.Query().All(ctx)
	if err != nil {
		l.Warning("Failed to query tasks for URI scheme upgrade: %s", err)
	}
	for _, task := range tasks {
		update := client.Task.UpdateOneID(task.ID)
		changed := false

		if strings.Contains(task.PrivateState, legacyURIScheme) {
			update.SetPrivateState(strings.ReplaceAll(task.PrivateState, legacyURIScheme, newPrefix))
			changed = true
		}

		if task.PublicState != nil {
			if raw, err := json.Marshal(task.PublicState); err == nil && strings.Contains(string(raw), legacyURIScheme) {
				raw = []byte(strings.ReplaceAll(string(raw), legacyURIScheme, newPrefix))
				var state types.TaskPublicState
				if err := json.Unmarshal(raw, &state); err == nil {
					update.SetPublicState(&state)
					changed = true
				}
			}
		}

		if !changed {
			continue
		}

		if err := update.Exec(ctx); err != nil {
			l.Warning("Failed to upgrade task %d URI scheme: %s", task.ID, err)
			continue
		}
		rewritten++
	}

	// 审计日志里记录的 URI（历史记录，同样是用户可见内容）。
	logs, err := client.AuditLog.Query().All(ctx)
	if err != nil {
		l.Warning("Failed to query audit logs for URI scheme upgrade: %s", err)
	}
	for _, entry := range logs {
		if entry.Content == nil {
			continue
		}
		raw, err := json.Marshal(entry.Content)
		if err != nil || !strings.Contains(string(raw), legacyURIScheme) {
			continue
		}

		var content map[string]interface{}
		if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), legacyURIScheme, newPrefix)), &content); err != nil {
			continue
		}

		if err := client.AuditLog.UpdateOneID(entry.ID).SetContent(content).Exec(ctx); err != nil {
			l.Warning("Failed to upgrade audit log %d URI scheme: %s", entry.ID, err)
			continue
		}
		rewritten++
	}

	// 打上标记，之后启动不再扫表。
	if err := client.Setting.Update().Where(setting.NameEQ(legacyURIMarker)).SetValue("1").Exec(ctx); err != nil {
		if err := client.Setting.Create().SetName(legacyURIMarker).SetValue("1").Exec(ctx); err != nil {
			l.Warning("Failed to record URI scheme upgrade marker: %s", err)
		}
	}

	if rewritten > 0 {
		l.Info("Rewrote %d legacy URI value(s) to the %s scheme.", rewritten, constants.StorFlowScheme)
	}
}

// upgradeOAuthClientBranding 把上游内置的 OAuth 客户端名称与图标换成 StorFlow 的。
func upgradeOAuthClientBranding(l logging.Logger, client *ent.Client, ctx context.Context) {
	clients, err := client.OAuthClient.Query().All(ctx)
	if err != nil {
		l.Warning("Failed to query OAuth clients for branding upgrade: %s", err)
		return
	}

	for _, c := range clients {
		update := client.OAuthClient.UpdateOneID(c.ID)
		changed := false

		if c.Name == legacyCLIClientName {
			update.SetName(storflowCLIClientName)
			changed = true
		}

		if c.Props != nil {
			if icon, ok := legacyIconPaths[c.Props.Icon]; ok {
				props := *c.Props
				props.Icon = icon
				update.SetProps(&props)
				changed = true
			}
		}

		if !changed {
			continue
		}

		if err := update.Exec(ctx); err != nil {
			l.Warning("Failed to upgrade OAuth client %d branding: %s", c.ID, err)
			continue
		}

		l.Info("Upgraded OAuth client %d branding to StorFlow.", c.ID)
	}
}
