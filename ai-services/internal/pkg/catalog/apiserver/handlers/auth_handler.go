package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/miq"
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
	accessToken := c.GetString(middleware.CtxRawTokenKey)
	if accessToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing token"})

		return
	}

	// Get refresh token from X-Refresh-Token header
	// TODO: Once the consumers starts providing refreshToken in the header, make it mandatory
	refreshToken := c.GetHeader("X-Refresh-Token")

	if err := h.svc.Logout(c.Request.Context(), accessToken, refreshToken); err != nil {
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

// TokenLogin godoc
//
//	@Summary		Exchange a ManageIQ token for a Catalog API JWT
//	@Description	IBM Power Mission Control and other ManageIQ-integrated products use this
//	@Description	endpoint to exchange a pre-existing ManageIQ token for an internal JWT
//	@Description	without supplying username/password credentials.
//	@Tags			Authentication
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	map[string]interface{}	"Returns access_token, refresh_token, and token_type"
//	@Failure		401	{object}	map[string]interface{}	"Invalid or expired ManageIQ token"
//	@Failure		403	{object}	map[string]interface{}	"ManageIQ token does not have required permissions"
//	@Failure		404	{object}	map[string]interface{}	"ManageIQ resource not found"
//	@Failure		503	{object}	map[string]interface{}	"ManageIQ unavailable or returned an unexpected server error"
//	@Router			/auth/token [post]
func (h *AuthHandler) TokenLogin(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	raw := strings.TrimPrefix(authHeader, "Bearer ")
	if !strings.HasPrefix(authHeader, "Bearer ") || raw == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing ManageIQ token in Authorization header"})

		return
	}

	access, refresh, err := h.svc.LoginWithToken(c.Request.Context(), raw)
	if err != nil {
		if errors.Is(err, miq.ErrUnauthorized) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired ManageIQ token"})

			return
		}
		var manageIQErr *miq.ManageIQError
		if errors.As(err, &manageIQErr) && manageIQErr.StatusCode >= 400 && manageIQErr.StatusCode < 500 {
			c.JSON(manageIQErr.StatusCode, gin.H{"error": manageIQErr.Message})

			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})

		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
	})
}
