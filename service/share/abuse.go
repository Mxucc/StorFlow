package share

import (
	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/pkg/eventtype"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/service/explorer"
	"github.com/gin-gonic/gin"
)

type (
	// AbuseReportSubmitService submits an abuse report for a share.
	// Anonymous visitors are allowed.
	AbuseReportSubmitService struct {
		ID     string `json:"id" binding:"required"`
		Reason string `json:"reason" binding:"required,max=2000"`
		Email  string `json:"email" binding:"omitempty,max=255"`
	}
	AbuseReportSubmitParamCtx struct{}
)

// Submit stores the abuse report.
func (service *AbuseReportSubmitService) Submit(c *gin.Context) error {
	dep := dependency.FromContext(c)

	shareID, err := dep.HashIDEncoder().Decode(service.ID, hashid.ShareID)
	if err != nil {
		return serializer.NewError(serializer.CodeParamErr, "Invalid share ID", err)
	}

	creator := dep.DBClient().AbuseReport.Create().
		SetShareID(shareID).
		SetReason(service.Reason).
		SetIP(c.ClientIP())
	if service.Email != "" {
		creator = creator.SetReporterEmail(service.Email)
	}
	if _, err := creator.Save(c); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to submit report", err)
	}

	explorer.WriteAudit(c, eventtype.ReportAbuse, "share", service.ID,
		map[string]interface{}{"reason": service.Reason})
	return nil
}
