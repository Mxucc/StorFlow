package user

import (
	"context"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/pkg/eventtype"
	"github.com/Mxucc/StorFlow/pkg/logging"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// WriteAuthAudit records authentication events (login / login failed) into
// the site-wide audit log. Best-effort, async. u may be nil for failed
// logins where the user could not be resolved.
func WriteAuthAudit(c *gin.Context, ev eventtype.EventType, u *ent.User, email string, err error) {
	dep := dependency.FromContext(c)
	if dep == nil {
		return
	}
	// Respect the per-event switches configured in the admin panel.
	if !dep.SettingProvider().AuditEventEnabled(c, ev) {
		return
	}
	db := dep.DBClient()
	l := logging.FromContext(c)

	ip := c.ClientIP()
	var userID int
	var userEmail string
	if u != nil {
		userID, userEmail = u.ID, u.Email
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		creator := db.AuditLog.Create().
			SetType(int(ev)).
			SetAction(ev.Name()).
			SetObjectType("user").
			SetObjectName(email).
			SetContent(map[string]interface{}{
				"email": email,
				"error": errDetail(err),
			}).
			SetIP(ip)
		if userID > 0 {
			creator = creator.SetUserID(userID).SetUserEmail(userEmail)
		}
		if _, e := creator.Save(ctx); e != nil {
			l.Warning("Failed to write auth audit (event=%s): %s", ev.Name(), e)
		}
	}()
}

func errDetail(err error) string {
	if err == nil {
		return ""
	}
	if appErr, ok := err.(*serializer.AppError); ok {
		return appErr.Msg
	}
	return err.Error()
}
