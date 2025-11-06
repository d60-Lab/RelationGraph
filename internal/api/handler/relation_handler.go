package handler

import (
    "strconv"

    "github.com/gin-gonic/gin"

    "github.com/d60-Lab/gin-template/pkg/response"
)

type followRequest struct {
    FromUserID string `json:"from_user_id" binding:"required"`
    ToUserID   string `json:"to_user_id" binding:"required"`
}

// Follow 建立关注（异步写粉丝表）
// @Summary 关注用户（异步冗余）
// @Tags 关系链
// @Accept json
// @Produce json
// @Param request body followRequest true "关注信息"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/v1/relations/follow [post]
func (h *Handler) Follow(c *gin.Context) {
    var req followRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.BadRequest(c, err.Error())
        return
    }
    if err := h.relService.Follow(c.Request.Context(), req.FromUserID, req.ToUserID); err != nil {
        response.BadRequest(c, err.Error())
        return
    }
    response.Success(c, nil)
}

// Unfollow 取消关注
// @Summary 取消关注
// @Tags 关系链
// @Accept json
// @Produce json
// @Param request body followRequest true "取消关注信息"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/v1/relations/unfollow [post]
func (h *Handler) Unfollow(c *gin.Context) {
    var req followRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.BadRequest(c, err.Error())
        return
    }
    if err := h.relService.Unfollow(c.Request.Context(), req.FromUserID, req.ToUserID); err != nil {
        response.BadRequest(c, err.Error())
        return
    }
    response.Success(c, nil)
}

// ListFollowing 查询某用户关注的人
// @Summary 查询关注列表
// @Tags 关系链
// @Param user_id path string true "用户ID"
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页数量" default(10)
// @Success 200 {object} response.Response{data=map[string]interface{}}
// @Router /api/v1/relations/{user_id}/following [get]
func (h *Handler) ListFollowing(c *gin.Context) {
    userID := c.Param("user_id")
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
    list, err := h.relService.ListFollowing(c.Request.Context(), userID, page, pageSize)
    if err != nil {
        response.InternalError(c, err)
        return
    }
    response.Success(c, gin.H{"page": page, "page_size": pageSize, "list": list})
}

// ListFans 查询某用户的粉丝
// @Summary 查询粉丝列表（来自冗余表）
// @Tags 关系链
// @Param user_id path string true "用户ID"
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页数量" default(10)
// @Success 200 {object} response.Response{data=map[string]interface{}}
// @Router /api/v1/relations/{user_id}/fans [get]
func (h *Handler) ListFans(c *gin.Context) {
    userID := c.Param("user_id")
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
    list, err := h.relService.ListFans(c.Request.Context(), userID, page, pageSize)
    if err != nil {
        response.InternalError(c, err)
        return
    }
    response.Success(c, gin.H{"page": page, "page_size": pageSize, "list": list})
}
