package explorer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/pkg/cluster/routes"
	"github.com/Mxucc/StorFlow/pkg/email"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/manager"
)

const (
	// quotaNotifyCachePrefix is the KV prefix of the per-user notification throttle.
	quotaNotifyCachePrefix = "user_quota_notified_"
	// quotaNotifyInterval is the minimum interval between two quota notifications
	// sent to the same user.
	quotaNotifyInterval = 24 * time.Hour
)

// notifyQuotaExceeded sends a throttled "storage is full" email when an operation
// is rejected because the user ran out of storage. Mail failures are logged only:
// the caller still reports the original error to the user.
func notifyQuotaExceeded(c context.Context, m manager.FileManager, err error) {
	if !errors.Is(err, fs.ErrInsufficientCapacity) {
		return
	}

	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	if dep == nil || user == nil || user.Email == "" {
		return
	}

	kv := dep.KV()
	throttleKey := fmt.Sprintf("%s%d", quotaNotifyCachePrefix, user.ID)
	if _, ok := kv.Get(throttleKey); ok {
		return
	}
	_ = kv.Set(throttleKey, time.Now().Unix(), int(quotaNotifyInterval.Seconds()))

	capacity, err := m.Capacity(c)
	if err != nil {
		dep.Logger().Warning("Failed to get capacity for quota notification: %s", err)
		return
	}

	settings := dep.SettingProvider()
	title, body, err := email.NewExceedQuotaEmail(c, settings, user,
		routes.MasterUserSettingsUrl(settings.SiteURL(c)).String(), capacity.Used, capacity.Total)
	if err != nil {
		dep.Logger().Warning("Failed to render quota notification email: %s", err)
		return
	}

	if err := dep.EmailClient(c).Send(c, user.Email, title, body); err != nil {
		dep.Logger().Warning("Failed to send quota notification email: %s", err)
	}
}
