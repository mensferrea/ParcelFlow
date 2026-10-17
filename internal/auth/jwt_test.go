package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"ParcelFlow/internal/auth"
	"ParcelFlow/internal/domain"
)

func TestTokenGenerationAndValidation(t *testing.T) {
	tm := auth.NewTokenManager("test-secret-key-12345", 1*time.Hour)
	userID := uuid.New()
	user := &domain.User{
		ID:    userID,
		Email: "test@example.com",
		Role:  domain.RoleCustomer,
	}

	token, err := tm.GenerateToken(user)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims, err := tm.ValidateToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("expected user ID %s, got %s", userID, claims.UserID)
	}
	if claims.Role != domain.RoleCustomer {
		t.Errorf("expected role CUSTOMER, got %s", claims.Role)
	}
}

func TestPasswordHashing(t *testing.T) {
	password := "SecretPass123"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	if !auth.CheckPassword(password, hash) {
		t.Error("expected password to match hash")
	}

	if auth.CheckPassword("WrongPassword", hash) {
		t.Error("expected wrong password to fail check")
	}
}
