package curd

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/lijianjunljj/gocommon/utils"
)

// Handler 操作层
type Handler struct {
	model  interface{}
	models interface{}
}

// Pages 分页列表数据结构
type Pages struct {
	Data     interface{} `json:"data"`
	Count    int64       `json:"count"`
	PageSize int         `json:"pageSize"`
	PageNum  int         `json:"pageNum"`
}

// NewHandler 实例化操作
func NewHandler(model interface{}, models ...interface{}) *Handler {
	var mods interface{}
	if len(models) >= 1 {
		mods = models[0]
	}
	return &Handler{
		model:  model,
		models: mods,
	}
}

// List 分页列表
func (h *Handler) List(ctx *gin.Context, isHook bool, extras ...Extra) {
	var search Search
	err := ctx.ShouldBindBodyWith(&search, binding.JSON)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	isSelf := ctx.GetBool("is_self")
	if isSelf {
		search.Conditions["user_id"] = ctx.GetString("userID")
	}
	svc := NewService(h.model)
	l := NewLogic(svc, h.models)
	count, err := l.List(&search, isHook, extras...)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}

	utils.Success(ctx, Pages{
		PageNum:  search.PageNum,
		PageSize: search.PageSize,
		Count:    count,
		Data:     h.models,
	})
}

// All 分页列表
func (h *Handler) All(ctx *gin.Context, extras ...Extra) {
	var search Search
	err := ctx.ShouldBindBodyWith(&search, binding.JSON)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	isSelf := ctx.GetBool("is_self")
	if isSelf {
		search.Conditions["user_id"] = ctx.GetString("userID")
	}
	svc := NewService(h.model)
	l := NewLogic(svc, h.models)
	err = l.All(&search, false, extras...)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	utils.Success(ctx, h.models)
}

// ListWithHook 分页列表带回调
func (h *Handler) ListWithHook(ctx *gin.Context, extras ...Extra) {
	var search Search
	err := ctx.ShouldBindBodyWith(&search, binding.JSON)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	isSelf := ctx.GetBool("is_self")
	if isSelf {
		search.Conditions["user_id"] = ctx.GetString("userID")
	}
	svc := NewService(h.model)
	l := NewLogic(svc, h.models)
	count, err := l.List(&search, true, extras...)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}

	utils.Success(ctx, Pages{
		PageNum:  search.PageNum,
		PageSize: search.PageSize,
		Count:    count,
		Data:     h.models,
	})
}

// AllWithHook 分页列表带回调
func (h *Handler) AllWithHook(ctx *gin.Context, extras ...Extra) {
	var search Search
	err := ctx.ShouldBindBodyWith(&search, binding.JSON)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	svc := NewService(h.model)
	l := NewLogic(svc, h.models)
	err = l.All(&search, true, extras...)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	utils.Success(ctx, h.models)
}

func (h *Handler) Add(ctx *gin.Context, extras ...Extra) {
	err := ctx.ShouldBindBodyWith(h.model, binding.JSON)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	svc := NewService(h.model)
	l := NewLogic(svc)
	userID, _ := ctx.Get("userID")

	err = l.Add(userID.(string), extras...)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	utils.Success(ctx, h.model)
}

// Edit 分页列表
func (h *Handler) Edit(ctx *gin.Context, extras ...Extra) {
	err := ctx.ShouldBindBodyWith(h.model, binding.JSON)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	svc := NewService(h.model)
	l := NewLogic(svc)
	err = l.Detail()
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	err = ctx.ShouldBindBodyWith(h.model, binding.JSON)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	err = l.Edit(extras...)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	utils.Success(ctx, h.model)
}

// Detail 详情
func (h *Handler) Detail(ctx *gin.Context, extras ...Extra) {
	err := ctx.ShouldBindBodyWith(&h.model, binding.JSON)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	svc := NewService(h.model)
	l := NewLogic(svc)
	err = l.Detail(extras...)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}

	utils.Success(ctx, h.model)
}

// Delete 删除
func (h *Handler) Delete(ctx *gin.Context, extras ...Extra) {
	params := make(map[string]interface{})
	err := ctx.ShouldBindBodyWith(&params, binding.JSON)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}
	svc := NewService(h.model)
	l := NewLogic(svc)
	err = l.Delete(fmt.Sprintf("%v", params["id"]), extras...)
	if err != nil {
		utils.Fail(ctx, err)
		return
	}

	utils.Success(ctx, h.model)
}
