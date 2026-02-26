package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
)

type AuthHandler struct {
	svc auth.Service
}

func NewAuthHandler(svc auth.Service) *AuthHandler {
	return &AuthHandler{svc: svc}
}

type loginReq struct {
	UserName string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login godoc
//
//	@Summary		User login
//	@Description	Authenticate user and return access and refresh tokens
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Param			credentials	body		loginReq				true	"Login credentials"
//	@Success		200			{object}	map[string]interface{}	"Returns access_token, refresh_token, and token_type"
//	@Failure		400			{object}	map[string]interface{}	"Invalid payload"
//	@Failure		401			{object}	map[string]interface{}	"Invalid credentials"
//	@Router			/auth/login [post].
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})

		return
	}

	access, refresh, err := h.svc.Login(c.Request.Context(), req.UserName, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})

		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
	})
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// Refresh godoc
//
//	@Summary		Refresh access token
//	@Description	Get new access and refresh tokens using a valid refresh token
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Param			refresh	body		refreshReq				true	"Refresh token"
//	@Success		200		{object}	map[string]interface{}	"Returns new access_token, refresh_token, and token_type"
//	@Failure		400		{object}	map[string]interface{}	"Invalid payload"
//	@Failure		401		{object}	map[string]interface{}	"Invalid refresh token"
//	@Router			/auth/refresh [post].
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})

		return
	}
	access, refresh, err := h.svc.RefreshTokens(c.Request.Context(), req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})

		return
	}
	c.JSON(http.StatusOK, gin.H{
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
	})
}

// Logout godoc
//
//	@Summary		User logout
//	@Description	Invalidate the current access token
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	map[string]interface{}	"Successfully logged out"
//	@Failure		400	{object}	map[string]interface{}	"Missing token"
//	@Failure		500	{object}	map[string]interface{}	"Failed to logout"
//	@Router			/auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	token := c.GetString(middleware.CtxRawTokenKey)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing token"})

		return
	}
	if err := h.svc.Logout(c.Request.Context(), token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to logout"})

		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// Me godoc
//
//	@Summary		Get current user info
//	@Description	Get information about the currently authenticated user
//	@Tags			Authentication
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	map[string]interface{}	"Returns user id, username, and name"
//	@Failure		401	{object}	map[string]interface{}	"Unauthorized"
//	@Failure		404	{object}	map[string]interface{}	"User not found"
//	@Router			/auth/me [get]
func (h *AuthHandler) Me(c *gin.Context) {
	userID := c.GetString(middleware.CtxUserIDKey)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})

		return
	}
	u, err := h.svc.GetUser(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})

		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":       u.ID,
		"username": u.UserName,
		"name":     u.Name,
	})
}
