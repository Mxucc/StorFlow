package controllers

import (
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/queue"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/service/explorer"
	"github.com/gin-gonic/gin"
)

func ListTasks(c *gin.Context) {
	service := ParametersFromContext[*explorer.ListTaskService](c, explorer.ListTaskParamCtx{})
	resp, err := service.ListTasks(c)
	if err != nil {
		c.JSON(200, serializer.Err(c, err))
		c.Abort()
		return
	}

	if resp != nil {
		c.JSON(200, serializer.Response{
			Data: resp,
		})
	}
}

func GetTaskPhaseProgress(c *gin.Context) {
	taskId := hashid.FromContext(c)
	resp, err := explorer.TaskPhaseProgress(c, taskId)
	if err != nil {
		c.JSON(200, serializer.Err(c, err))
		c.Abort()
		return
	}

	if resp != nil {
		c.JSON(200, serializer.Response{
			Data: resp,
		})
	} else {
		c.JSON(200, serializer.Response{Data: queue.Progresses{}})
	}
}

func SetDownloadTaskTarget(c *gin.Context) {
	taskId := hashid.FromContext(c)
	service := ParametersFromContext[*explorer.SetDownloadFilesService](c, explorer.SetDownloadFilesParamCtx{})
	err := service.SetDownloadFiles(c, taskId)
	if err != nil {
		c.JSON(200, serializer.Err(c, err))
		c.Abort()
		return
	}

	c.JSON(200, serializer.Response{})
}

func CancelDownloadTask(c *gin.Context) {
	taskId := hashid.FromContext(c)
	err := explorer.CancelDownloadTask(c, taskId)
	if err != nil {
		c.JSON(200, serializer.Err(c, err))
		c.Abort()
		return
	}

	c.JSON(200, serializer.Response{})
}

// ListAvailableNodes 返回当前用户组可选的可用节点（Pro: 允许选择节点）。
func ListAvailableNodes(c *gin.Context) {
	resp, err := explorer.ListAvailableNodes(c)
	if err != nil {
		c.JSON(200, serializer.Err(c, err))
		c.Abort()
		return
	}

	c.JSON(200, serializer.Response{
		Data: resp,
	})
}
