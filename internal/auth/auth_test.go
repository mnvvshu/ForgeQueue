package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/forgequeue/forgequeue/internal/auth"
)

func TestPasswordHashing(t *testing.T) {
	password := "SecureSuperSecret123!"

	// Hash password
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	// Verify valid password
	match, err := auth.ComparePassword(hash, password)
	if err != nil || !match {
		t.Fatalf("expected password match, match=%v, err=%v", match, err)
	}

	// Verify invalid password
	wrongMatch, err := auth.ComparePassword(hash, "WrongPassword!")
	if err != nil {
		t.Fatalf("unexpected error comparing wrong password: %v", err)
	}
	if wrongMatch {
		t.Fatalf("expected wrong password to fail matching")
	}

	// Password too short
	_, err = auth.HashPassword("short")
	if err == nil {
		t.Fatalf("expected error for password < 8 characters")
	}
}

func TestJWTTokenManager(t *testing.T) {
	secret := "a_very_secure_secret_key_that_is_at_least_32_bytes_long!"
	tm, err := auth.NewTokenManager(secret, "forgequeue-test")
	if err != nil {
		t.Fatalf("failed to create TokenManager: %v", err)
	}

	userID := "usr-123"
	username := "alice"

	// 1. Generate and validate valid token
	tokenStr, err := tm.GenerateToken(userID, username, 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := tm.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}
	if claims.UserID != userID || claims.Username != username {
		t.Fatalf("claims mismatch: %+v", claims)
	}

	// 2. Context propagation
	ctx := auth.WithUserContext(context.Background(), claims)
	extracted, ok := auth.UserFromContext(ctx)
	if !ok || extracted.UserID != userID {
		t.Fatalf("failed to extract user from context")
	}

	// 3. Expired token validation
	expiredToken, err := tm.GenerateToken(userID, username, -1*time.Second)
	if err != nil {
		t.Fatalf("failed to generate expired token: %v", err)
	}
	_, err = tm.ValidateToken(expiredToken)
	if err == nil {
		t.Fatalf("expected expired token to fail validation")
	}

	// 4. Insecure short secret rejection
	_, err = auth.NewTokenManager("too_short", "test")
	if err == nil {
		t.Fatalf("expected error for secret < 32 characters")
	}
}
