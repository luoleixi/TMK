package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"tmk-glance/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	contextUserKey      = "authenticated_user"
	tokenKindAccess     = "access"
	tokenKindRefresh    = "refresh"
	maxLoginFailures    = 5
	loginFailureWindow  = 15 * time.Minute
	maxLoginAttemptKeys = 10000
)

var invalidPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("invalid-password-placeholder"), bcrypt.DefaultCost)

type loginAttempt struct {
	Failures    int
	WindowStart time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

type tokenPair struct {
	AccessToken      string     `json:"access_token"`
	RefreshToken     string     `json:"refresh_token"`
	TokenType        string     `json:"token_type"`
	ExpiresInSeconds int        `json:"expires_in_seconds"`
	User             model.User `json:"user"`
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt)}
}

func (l *loginLimiter) allowed(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt, ok := l.attempts[key]
	if !ok || now.Sub(attempt.WindowStart) >= loginFailureWindow {
		delete(l.attempts, key)
		return true
	}
	return attempt.Failures < maxLoginFailures
}

func (l *loginLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.attempts) >= maxLoginAttemptKeys {
		for candidate, value := range l.attempts {
			if now.Sub(value.WindowStart) >= loginFailureWindow {
				delete(l.attempts, candidate)
			}
		}
	}
	if len(l.attempts) >= maxLoginAttemptKeys {
		return
	}
	attempt := l.attempts[key]
	if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) >= loginFailureWindow {
		attempt = loginAttempt{WindowStart: now}
	}
	attempt.Failures++
	l.attempts[key] = attempt
}

func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (a *Application) bootstrapAdmin() error {
	email := normalizeEmail(a.cfg.Auth.BootstrapAdminEmail)
	password := a.cfg.Auth.BootstrapAdminPassword
	count, err := a.store.UserCount()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if email == "" && password == "" {
		return errors.New("no users exist; set AUTH_BOOTSTRAP_ADMIN_EMAIL and AUTH_BOOTSTRAP_ADMIN_PASSWORD")
	}
	if email == "" || password == "" {
		return errors.New("both bootstrap admin email and password are required")
	}
	if err := validateEmail(email); err != nil {
		return fmt.Errorf("bootstrap admin email: %w", err)
	}
	if err := validatePassword(password); err != nil {
		return fmt.Errorf("bootstrap admin password: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return a.store.CreateUser(&model.User{
		ID: uuid.NewString(), Email: email, DisplayName: "Administrator", PasswordHash: string(hash),
		Role: model.RoleAdmin, Status: model.UserStatusActive, MustChangePassword: true,
		CreatedAt: now, UpdatedAt: now,
	})
}

func (a *Application) handleLogin(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "email and password are required"})
		return
	}
	req.Email = normalizeEmail(req.Email)
	emailKey := "email|" + req.Email
	ipKey := "ip|" + c.ClientIP()
	if !a.loginLimiter.allowed(emailKey, time.Now()) || !a.loginLimiter.allowed(ipKey, time.Now()) {
		a.audit(c, "", "auth.login", "user", "", "denied", gin.H{"reason": "rate_limited"})
		c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "message": "too many login attempts; try again later"})
		return
	}
	user, ok, err := a.store.GetUserByEmail(req.Email)
	if err != nil {
		log.Printf("[auth] lookup user failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "login failed"})
		return
	}
	passwordHash := invalidPasswordHash
	if ok {
		passwordHash = []byte(user.PasswordHash)
	}
	passwordMatches := bcrypt.CompareHashAndPassword(passwordHash, []byte(req.Password)) == nil
	if !ok || user.Status != model.UserStatusActive || !passwordMatches {
		a.loginLimiter.fail(emailKey, time.Now())
		a.loginLimiter.fail(ipKey, time.Now())
		a.audit(c, userIDOrEmpty(user), "auth.login", "user", userIDOrEmpty(user), "denied", gin.H{"reason": "invalid_credentials"})
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid email or password"})
		return
	}
	pair, err := a.issueTokenPair(user)
	if err != nil {
		log.Printf("[auth] issue tokens failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "login failed"})
		return
	}
	a.loginLimiter.success(emailKey)
	a.loginLimiter.success(ipKey)
	if err := a.store.MarkLogin(user.ID); err != nil {
		log.Printf("[auth] update last login failed: %v", err)
	}
	a.audit(c, user.ID, "auth.login", "user", user.ID, "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": pair})
}

func (a *Application) handleRefresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.RefreshToken) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "refresh_token is required"})
		return
	}
	oldHash := hashToken(req.RefreshToken)
	user, ok, err := a.store.ResolveToken(oldHash, tokenKindRefresh, time.Now())
	if err != nil {
		log.Printf("[auth] resolve refresh token failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "refresh failed"})
		return
	}
	if !ok || user.Status != model.UserStatusActive {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid or expired refresh token"})
		return
	}
	pair, access, refresh, err := a.buildTokenPair(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "refresh failed"})
		return
	}
	rotated, err := a.store.RotateRefreshToken(oldHash, access, refresh)
	if err != nil {
		log.Printf("[auth] rotate refresh token failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "refresh failed"})
		return
	}
	if !rotated {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid or expired refresh token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": pair})
}

func (a *Application) handleLogout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&req)
	if token := bearerToken(c); token != "" {
		_ = a.store.RevokeToken(hashToken(token))
	}
	if req.RefreshToken != "" {
		_ = a.store.RevokeToken(hashToken(req.RefreshToken))
	}
	user := currentUser(c)
	a.audit(c, user.ID, "auth.logout", "user", user.ID, "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

func (a *Application) handleMe(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": currentUser(c)})
}

func (a *Application) handleChangePassword(c *gin.Context) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "current_password and new_password are required"})
		return
	}
	if err := validatePassword(req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	user := currentUser(c)
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "current password is incorrect"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil || a.store.UpdatePassword(user.ID, string(hash), false) != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "change password failed"})
		return
	}
	a.audit(c, user.ID, "auth.password.change", "user", user.ID, "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "password changed; sign in again"})
}

func (a *Application) handleListUsers(c *gin.Context) {
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	users, total, err := a.store.ListUsers(limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "list users failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"total": total, "users": users}})
}

func (a *Application) handleCreateUser(c *gin.Context) {
	var req struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		Role        string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || validateEmail(req.Email) != nil || validatePassword(req.Password) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "valid email and password of 12-72 bytes are required"})
		return
	}
	if !validRole(req.Role) {
		req.Role = model.RoleUser
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid password"})
		return
	}
	now := time.Now().UTC()
	user := &model.User{ID: uuid.NewString(), Email: normalizeEmail(req.Email), DisplayName: strings.TrimSpace(req.DisplayName),
		PasswordHash: string(hash), Role: req.Role, Status: model.UserStatusActive,
		MustChangePassword: true, CreatedAt: now, UpdatedAt: now}
	if err := a.store.CreateUser(user); err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "email already exists"})
		return
	}
	actor := currentUser(c)
	a.audit(c, actor.ID, "admin.user.create", "user", user.ID, "success", gin.H{"role": user.Role})
	c.JSON(http.StatusCreated, gin.H{"code": 0, "message": "ok", "data": user})
}

func (a *Application) handleUpdateUser(c *gin.Context) {
	var req struct {
		DisplayName        string `json:"display_name"`
		Role               string `json:"role"`
		Status             string `json:"status"`
		MustChangePassword bool   `json:"must_change_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !validRole(req.Role) || !validStatus(req.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "valid display_name, role and status are required"})
		return
	}
	target, ok, err := a.store.GetUserByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "update user failed"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "user not found"})
		return
	}
	updated, lastAdminConflict, err := a.store.UpdateUserWithAdminGuard(target.ID, strings.TrimSpace(req.DisplayName), req.Role, req.Status, req.MustChangePassword)
	if lastAdminConflict {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "cannot disable or demote the last active admin"})
		return
	}
	if err != nil || !updated {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "update user failed"})
		return
	}
	if req.Status != model.UserStatusActive || req.Role != target.Role {
		_ = a.store.RevokeUserTokens(target.ID)
	}
	actor := currentUser(c)
	a.audit(c, actor.ID, "admin.user.update", "user", target.ID, "success", gin.H{"role": req.Role, "status": req.Status})
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

func (a *Application) handleResetUserPassword(c *gin.Context) {
	var req struct {
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || validatePassword(req.NewPassword) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "new_password must be 12-72 bytes"})
		return
	}
	target, ok, err := a.store.GetUserByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "reset password failed"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "user not found"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil || a.store.UpdatePassword(target.ID, string(hash), true) != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "reset password failed"})
		return
	}
	actor := currentUser(c)
	a.audit(c, actor.ID, "admin.user.password.reset", "user", target.ID, "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "password reset; user must change it at next login"})
}

func (a *Application) handleClaimLegacySessions(c *gin.Context) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.UserID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "user_id is required"})
		return
	}
	if _, ok, err := a.store.GetUserByID(req.UserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "claim legacy sessions failed"})
		return
	} else if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "user not found"})
		return
	}
	claimed, err := a.store.ClaimUnownedSessions(req.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "claim legacy sessions failed"})
		return
	}
	actor := currentUser(c)
	a.audit(c, actor.ID, "admin.session.claim_legacy", "user", req.UserID, "success", gin.H{"claimed": claimed})
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"claimed": claimed}})
}

func (a *Application) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "authorization required"})
			return
		}
		user, ok, err := a.store.ResolveToken(hashToken(token), tokenKindAccess, time.Now())
		if err != nil {
			log.Printf("[auth] resolve access token failed: %v", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "authentication failed"})
			return
		}
		if !ok || user.Status != model.UserStatusActive {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid or expired access token"})
			return
		}
		c.Set(contextUserKey, user)
		c.Next()
	}
}

func requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if currentUser(c).Role != model.RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "message": "admin permission required"})
			return
		}
		c.Next()
	}
}

func requirePasswordReady() gin.HandlerFunc {
	return func(c *gin.Context) {
		if currentUser(c).MustChangePassword {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "message": "password change required", "reason": "password_change_required"})
			return
		}
		c.Next()
	}
}

func (a *Application) issueTokenPair(user *model.User) (tokenPair, error) {
	pair, access, refresh, err := a.buildTokenPair(user)
	if err != nil {
		return tokenPair{}, err
	}
	if err := a.store.CreateToken(access); err != nil {
		return tokenPair{}, err
	}
	if err := a.store.CreateToken(refresh); err != nil {
		_ = a.store.RevokeToken(access.TokenHash)
		return tokenPair{}, err
	}
	return pair, nil
}

func (a *Application) buildTokenPair(user *model.User) (tokenPair, model.AuthToken, model.AuthToken, error) {
	accessValue, err := randomToken()
	if err != nil {
		return tokenPair{}, model.AuthToken{}, model.AuthToken{}, err
	}
	refreshValue, err := randomToken()
	if err != nil {
		return tokenPair{}, model.AuthToken{}, model.AuthToken{}, err
	}
	now := time.Now().UTC()
	accessMinutes := a.cfg.Auth.AccessTokenTTLMinutes
	if accessMinutes < 1 || accessMinutes > 60 {
		accessMinutes = 15
	}
	refreshDays := a.cfg.Auth.RefreshTokenTTLDays
	if refreshDays < 1 || refreshDays > 90 {
		refreshDays = 30
	}
	accessTTL := time.Duration(accessMinutes) * time.Minute
	refreshTTL := time.Duration(refreshDays) * 24 * time.Hour
	access := model.AuthToken{ID: uuid.NewString(), UserID: user.ID, Kind: tokenKindAccess, TokenHash: hashToken(accessValue), ExpiresAt: now.Add(accessTTL), CreatedAt: now}
	refresh := model.AuthToken{ID: uuid.NewString(), UserID: user.ID, Kind: tokenKindRefresh, TokenHash: hashToken(refreshValue), ExpiresAt: now.Add(refreshTTL), CreatedAt: now}
	pair := tokenPair{AccessToken: accessValue, RefreshToken: refreshValue, TokenType: "Bearer", ExpiresInSeconds: int(accessTTL.Seconds()), User: *user}
	return pair, access, refresh, nil
}

func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func bearerToken(c *gin.Context) string {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func currentUser(c *gin.Context) *model.User {
	value, ok := c.Get(contextUserKey)
	if !ok {
		return &model.User{}
	}
	user, _ := value.(*model.User)
	if user == nil {
		return &model.User{}
	}
	return user
}

func normalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func validateEmail(value string) error {
	value = normalizeEmail(value)
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || len(value) > 254 {
		return errors.New("invalid email")
	}
	return nil
}

func validatePassword(value string) error {
	if len(value) < 12 || len(value) > 72 {
		return errors.New("password must be 12-72 bytes")
	}
	return nil
}

func validRole(value string) bool { return value == model.RoleUser || value == model.RoleAdmin }
func validStatus(value string) bool {
	return value == model.UserStatusActive || value == model.UserStatusDisabled
}

func userIDOrEmpty(user *model.User) string {
	if user == nil {
		return ""
	}
	return user.ID
}

func (a *Application) audit(c *gin.Context, actorID, action, resourceType, resourceID, result string, details any) {
	encoded := []byte("{}")
	if details != nil {
		if value, err := json.Marshal(details); err == nil {
			encoded = value
		}
	}
	event := model.AuditEvent{ActorUserID: actorID, Action: action, ResourceType: resourceType,
		ResourceID: resourceID, IPAddress: c.ClientIP(), UserAgent: c.Request.UserAgent(), Result: result,
		DetailsJSON: string(encoded), CreatedAt: time.Now().UTC()}
	if err := a.store.WriteAudit(event); err != nil {
		log.Printf("[audit] write failed: %v", err)
	}
}
