package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/forgequeue/forgequeue/internal/auth"
	"github.com/forgequeue/forgequeue/internal/domain"
	"github.com/forgequeue/forgequeue/internal/repository"
)

type AuthHandler struct {
	userRepo     repository.UserRepository
	tokenManager *auth.TokenManager
	tokenTTL     time.Duration
}

func NewAuthHandler(userRepo repository.UserRepository, tm *auth.TokenManager, tokenTTL time.Duration) *AuthHandler {
	if tokenTTL <= 0 {
		tokenTTL = 24 * time.Hour
	}
	return &AuthHandler{
		userRepo:     userRepo,
		tokenManager: tm,
		tokenTTL:     tokenTTL,
	}
}

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	User  UserResponse `json:"user"`
	Token string       `json:"token"`
}

type UserResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON payload")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if len(req.Username) < 3 || len(req.Username) > 32 {
		RespondError(w, http.StatusBadRequest, "INVALID_USERNAME", "Username must be between 3 and 32 characters")
		return
	}
	if !strings.Contains(req.Email, "@") || !strings.Contains(req.Email, ".") {
		RespondError(w, http.StatusBadRequest, "INVALID_EMAIL", "Invalid email address format")
		return
	}
	if len(req.Password) < 8 {
		RespondError(w, http.StatusBadRequest, "INVALID_PASSWORD", "Password must be at least 8 characters long")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "SYSTEM_ERROR", "Failed to secure password")
		return
	}

	now := time.Now()
	user := &domain.User{
		ID:           uuid.New().String(),
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.userRepo.Create(r.Context(), user); err != nil {
		RespondError(w, http.StatusConflict, "USER_EXISTS", err.Error())
		return
	}

	token, err := h.tokenManager.GenerateToken(user.ID, user.Username, h.tokenTTL)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "SYSTEM_ERROR", "Failed to issue authentication token")
		return
	}

	RespondJSON(w, http.StatusCreated, AuthResponse{
		User: UserResponse{
			ID:        user.ID,
			Username:  user.Username,
			Email:     user.Email,
			CreatedAt: user.CreatedAt,
		},
		Token: token,
	})
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON payload")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		RespondError(w, http.StatusBadRequest, "MISSING_CREDENTIALS", "Username and password are required")
		return
	}

	// Lookup user by username or email
	user, err := h.userRepo.GetByUsername(r.Context(), req.Username)
	if err != nil {
		user, err = h.userRepo.GetByEmail(r.Context(), strings.ToLower(req.Username))
	}

	if err != nil || user == nil {
		RespondError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid username or password")
		return
	}

	match, err := auth.ComparePassword(user.PasswordHash, req.Password)
	if err != nil || !match {
		RespondError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid username or password")
		return
	}

	token, err := h.tokenManager.GenerateToken(user.ID, user.Username, h.tokenTTL)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "SYSTEM_ERROR", "Failed to issue authentication token")
		return
	}

	RespondJSON(w, http.StatusOK, AuthResponse{
		User: UserResponse{
			ID:        user.ID,
			Username:  user.Username,
			Email:     user.Email,
			CreatedAt: user.CreatedAt,
		},
		Token: token,
	})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok || claims == nil {
		RespondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated")
		return
	}

	user, err := h.userRepo.GetByID(r.Context(), claims.UserID)
	if err != nil {
		RespondError(w, http.StatusNotFound, "NOT_FOUND", "User not found")
		return
	}

	RespondJSON(w, http.StatusOK, UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	})
}
