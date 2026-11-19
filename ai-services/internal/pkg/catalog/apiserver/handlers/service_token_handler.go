package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const jwksCacheControl = "public, max-age=300"

// ServiceTokenHandler serves the JWKS and the service-token, authorize and exchange endpoints.
type ServiceTokenHandler struct {
	tokens *auth.TokenService
	signer *auth.Signer
}

// NewServiceTokenHandler creates the handler.
func NewServiceTokenHandler(tokens *auth.TokenService, signer *auth.Signer) *ServiceTokenHandler {
	return &ServiceTokenHandler{tokens: tokens, signer: signer}
}

// JWKS godoc
//
//	@Summary		JSON Web Key Set
//	@Description	Public keys that verify service tokens. Contains no private members.
//	@Tags			Authentication
//	@Produce		json
//	@Success		200	{object}	auth.JWKS
//	@Router			/.well-known/jwks.json [get]
func (h *ServiceTokenHandler) JWKS(c *gin.Context) {
	set, err := h.signer.JWKS(c.Request.Context())
	if err != nil {
		logger.ErrorfCtx(c.Request.Context(), "jwks: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})

		return
	}
	c.Header("Cache-Control", jwksCacheControl)
	c.JSON(http.StatusOK, set)
}

type serviceTokenReq struct {
	Service string `json:"service" binding:"required"`
}

type tokenResp struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// ServiceToken godoc
//
//	@Summary		Issue a service token
//	@Description	Exchanges the caller's catalog access token for a short-lived ES256 token for one service.
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		serviceTokenReq	true	"Service name"
//	@Success		200		{object}	tokenResp
//	@Failure		400		{object}	map[string]interface{}	"Unknown or missing service"
//	@Failure		403		{object}	map[string]interface{}	"No access to the service"
//	@Router			/auth/service-token [post]
func (h *ServiceTokenHandler) ServiceToken(c *gin.Context) {
	var req serviceTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})

		return
	}

	token, exp, err := h.tokens.IssueServiceToken(c.Request.Context(), c.GetString(middleware.CtxUserIDKey), req.Service)
	if err != nil {
		h.writeError(c, err)

		return
	}
	c.JSON(http.StatusOK, tokenResp{Token: token, ExpiresAt: exp.UTC().Format("2006-01-02T15:04:05Z")})
}

type authorizeReq struct {
	Service             string `json:"service" binding:"required"`
	RedirectURI         string `json:"redirect_uri" binding:"required"`
	CodeChallenge       string `json:"code_challenge" binding:"required"`
	CodeChallengeMethod string `json:"code_challenge_method" binding:"required"`
	State               string `json:"state" binding:"required"`
}

// Authorize godoc
//
//	@Summary		Authorize a service UI sign-in
//	@Description	Called by the catalog UI for a signed-in user. Returns the URL to send the browser to, carrying a one-time code.
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		authorizeReq	true	"Authorize request"
//	@Success		200		{object}	map[string]string	"redirect_to"
//	@Failure		400		{object}	map[string]interface{}	"Malformed request or unregistered redirect_uri"
//	@Failure		403		{object}	map[string]interface{}	"No access to the service"
//	@Router			/auth/authorize [post]
func (h *ServiceTokenHandler) Authorize(c *gin.Context) {
	var req authorizeReq
	if err := c.ShouldBindJSON(&req); err != nil || req.CodeChallengeMethod != "S256" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})

		return
	}

	to, err := h.tokens.Authorize(c.Request.Context(), c.GetString(middleware.CtxUserIDKey), auth.AuthorizeRequest{
		Service: req.Service, RedirectURI: req.RedirectURI, CodeChallenge: req.CodeChallenge, State: req.State,
	})
	if err != nil {
		h.writeError(c, err)

		return
	}
	c.JSON(http.StatusOK, gin.H{"redirect_to": to})
}

type exchangeReq struct {
	Service      string `json:"service" binding:"required"`
	RedirectURI  string `json:"redirect_uri" binding:"required"`
	Code         string `json:"code" binding:"required"`
	CodeVerifier string `json:"code_verifier" binding:"required"`
}

// Exchange godoc
//
//	@Summary		Exchange an authorization code
//	@Description	Trades a one-time code and its PKCE verifier for a service token. The code plus verifier are the credential. Every failure returns the same error.
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Param			request	body		exchangeReq	true	"Exchange request"
//	@Success		200		{object}	tokenResp
//	@Failure		400		{object}	map[string]interface{}	"Invalid grant"
//	@Router			/auth/code/exchange [post]
func (h *ServiceTokenHandler) Exchange(c *gin.Context) {
	var req exchangeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid grant"})

		return
	}

	token, exp, err := h.tokens.Exchange(c.Request.Context(), auth.ExchangeRequest{
		Service: req.Service, RedirectURI: req.RedirectURI, Code: req.Code, CodeVerifier: req.CodeVerifier,
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidGrant) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid grant"})

			return
		}
		logger.ErrorfCtx(c.Request.Context(), "code exchange: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})

		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, tokenResp{Token: token, ExpiresAt: exp.UTC().Format("2006-01-02T15:04:05Z")})
}

// writeError maps token-service errors to HTTP statuses. Unexpected errors are logged and
// reported as a generic 500 with no internal detail.
func (h *ServiceTokenHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, auth.ErrUnknownService), errors.Is(err, auth.ErrInvalidRequest), errors.Is(err, auth.ErrInvalidRedirect):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	case errors.Is(err, auth.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	default:
		logger.ErrorfCtx(c.Request.Context(), "service token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}
