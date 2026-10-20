package explorer

import (
	"context"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/pkg/eventtype"
	"github.com/Mxucc/StorFlow/pkg/logging"
	"github.com/gin-gonic/gin"
)

// WriteAudit records a user operation into the site-wide audit log
// (best-effort, async - never blocks the main operation).
// Event types and names are aligned with the Pro edition.
func WriteAudit(c *gin.Context, ev eventtype.EventType, objType, objName string, content map[string]interface{}) {
	dep := dependency.FromContext(c)
	if dep == nil {
		return
	}
	// Respect the per-event switches configured in the admin panel.
	if !dep.SettingProvider().AuditEventEnabled(c, ev) {
		return
	}
	user := inventory.UserFromContext(c)
	db := dep.DBClient()
	l := logging.FromContext(c)

	userID, userEmail := 0, ""
	if user != nil {
		userID, userEmail = user.ID, user.Email
	}
	ip := c.ClientIP()
	objectName := objName
	if content == nil {
		content = map[string]interface{}{}
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		creator := db.AuditLog.Create().
			SetType(int(ev)).
			SetAction(ev.Name()).
			SetObjectType(objType).
			SetObjectName(objectName).
			SetContent(content).
			SetIP(ip)
		if userID > 0 {
			creator = creator.SetUserID(userID).SetUserEmail(userEmail)
		}
		if _, err := creator.Save(ctx); err != nil {
			l.Warning("Failed to write audit log (action=%s): %s", ev.Name(), err)
		}
	}()
}
