package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/wmusermii/dms-go-service/internal/auth"
)

type AuthHandler struct {
	tokenService *auth.TokenService
}

func NewAuthHandler(tokenService *auth.TokenService) *AuthHandler {
	return &AuthHandler{
		tokenService: tokenService,
	}
}

type IssueTokenRequest struct {
	UserID         string `json:"userId"`
	MerchantID     string `json:"merchantId"`
	RegistrationID string `json:"registrationId"`
	Role           string `json:"role"`
}

type IssueTokenResponse struct {
	AccessToken string    `json:"accessToken"`
	TokenType   string    `json:"tokenType"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func (h *AuthHandler) IssueToken(w http.ResponseWriter, r *http.Request) {
	var request IssueTokenRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.UserID == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}

	allowedRoles := map[string]bool{
		"MERCHANT":              true,
		"MERCHANT_REGISTRATION": true,
	}

	if !allowedRoles[request.Role] {
		writeError(w, http.StatusBadRequest, "invalid role")
		return
	}

	if request.Role == "MERCHANT_REGISTRATION" && request.RegistrationID == "" {
		writeError(w, http.StatusBadRequest, "registrationId is required")
		return
	}

	if request.Role == "MERCHANT" && request.MerchantID == "" {
		writeError(w, http.StatusBadRequest, "merchantId is required")
		return
	}

	accessToken, expiresAt, err := h.tokenService.IssueToken(
		request.UserID,
		request.MerchantID,
		request.RegistrationID,
		request.Role,
	)

	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	writeJSON(w, http.StatusOK, IssueTokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
	})
}
