package user

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceAuthorization contains the link the human must open. The device code
// remains private; retain the returned value to pass to CompleteLogin.
type DeviceAuthorization struct {
	VerificationURI           string
	VerificationURIComplete   string
	UserCode                  string
	ExpiresAt                 time.Time
	Interval                  time.Duration
	deviceCode, appID, issuer string
}

func (d DeviceAuthorization) String() string {
	return "feishu/user: device authorization (device code redacted)"
}
func (d DeviceAuthorization) GoString() string { return d.String() }

func (c *Client) StartLogin(ctx context.Context) (DeviceAuthorization, error) {
	form := url.Values{"client_id": {c.appID}, "scope": {strings.Join(c.scopes, " ")}}
	started := c.now()
	data, status, _, err := c.send(ctx, http.MethodPost, c.accountsURL+"/oauth/v1/device_authorization", "application/x-www-form-urlencoded", []byte(form.Encode()), "Basic "+base64.StdEncoding.EncodeToString([]byte(c.appID+":"+c.appSecret)))
	if err != nil {
		return DeviceAuthorization{}, err
	}
	if err := oauthFailure(data, status); err != nil {
		return DeviceAuthorization{}, err
	}
	var wire struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int64  `json:"expires_in"`
		Interval                int64  `json:"interval"`
	}
	if json.Unmarshal(data, &wire) != nil || wire.DeviceCode == "" || !validSeconds(wire.ExpiresIn) {
		return DeviceAuthorization{}, ErrInvalidResponse
	}
	if wire.Interval == 0 {
		wire.Interval = 5
	}
	if !validSeconds(wire.Interval) {
		return DeviceAuthorization{}, ErrInvalidResponse
	}
	if wire.VerificationURIComplete == "" {
		wire.VerificationURIComplete = wire.VerificationURI
	}
	for _, link := range []string{wire.VerificationURI, wire.VerificationURIComplete} {
		u, err := url.Parse(link)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
			return DeviceAuthorization{}, ErrInvalidResponse
		}
	}
	return DeviceAuthorization{VerificationURI: wire.VerificationURI, VerificationURIComplete: wire.VerificationURIComplete, UserCode: wire.UserCode, ExpiresAt: started.Add(time.Duration(wire.ExpiresIn) * time.Second), Interval: time.Duration(wire.Interval) * time.Second, deviceCode: wire.DeviceCode, appID: c.appID, issuer: c.accountsURL}, nil
}

// CompleteLogin polls after each server-requested interval, retrieves the user
// identity and persists the authorized session. It never opens a browser itself.
func (c *Client) CompleteLogin(ctx context.Context, auth DeviceAuthorization) (UserInfo, error) {
	if ctx == nil {
		return UserInfo{}, errors.New("feishu/user: context is required")
	}
	if auth.deviceCode == "" || auth.appID != c.appID || auth.issuer != c.accountsURL || auth.Interval <= 0 {
		return UserInfo{}, errors.New("feishu/user: invalid or mismatched device authorization")
	}
	if !c.now().Before(auth.ExpiresAt) {
		return UserInfo{}, ErrDeviceExpired
	}
	ctx, cancel := context.WithDeadline(ctx, auth.ExpiresAt)
	defer cancel()
	interval := auth.Interval
	attempt := 0
	for {
		attempt++
		if c.onPollTick != nil {
			if err := c.onPollTick(ctx, PollTick{Attempt: attempt, Interval: interval, ExpiresAt: auth.ExpiresAt}); err != nil {
				return UserInfo{}, err
			}
		}
		if err := c.wait(ctx, interval); err != nil {
			if !c.now().Before(auth.ExpiresAt) {
				return UserInfo{}, ErrDeviceExpired
			}
			return UserInfo{}, err
		}
		if err := ctx.Err(); err != nil {
			return UserInfo{}, err
		}
		form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {auth.deviceCode}, "client_id": {c.appID}, "client_secret": {c.appSecret}}
		issued := c.now()
		data, status, _, err := c.send(ctx, http.MethodPost, c.accountsURL+"/oauth/v3/token", "application/x-www-form-urlencoded", []byte(form.Encode()), "")
		if err != nil {
			return UserInfo{}, err
		}
		if err := oauthFailure(data, status); err != nil {
			var oauthErr *OAuthError
			if errors.As(err, &oauthErr) {
				switch oauthErr.ErrorCode {
				case "authorization_pending":
					continue
				case "slow_down":
					if interval > time.Duration(math.MaxInt64)-5*time.Second {
						return UserInfo{}, ErrInvalidResponse
					}
					interval += 5 * time.Second
					continue
				case "expired_token":
					return UserInfo{}, errors.Join(ErrDeviceExpired, err)
				}
			}
			return UserInfo{}, err
		}
		token, err := c.decodeToken(data, Token{}, issued)
		if err != nil {
			return UserInfo{}, err
		}
		if err := c.checkScopes(token.Scope); err != nil {
			return UserInfo{}, err
		}
		var identity UserInfo
		if err := c.apiWithToken(ctx, token.AccessToken, http.MethodGet, "/authen/v1/user_info", nil, &identity); err != nil {
			return UserInfo{}, err
		}
		if identity.OpenID == "" {
			return UserInfo{}, ErrInvalidResponse
		}
		token.OpenID, token.Name = identity.OpenID, identity.Name
		if err := c.lock(ctx); err != nil {
			return UserInfo{}, err
		}
		// Commit issued credentials even if the caller cancels at this point.
		saveCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), c.timeout)
		err = c.persist(saveCtx, token)
		stop()
		c.unlock()
		if err != nil {
			return UserInfo{}, err
		}
		return identity, nil
	}
}

type UserInfo struct {
	OpenID    string `json:"open_id"`
	UnionID   string `json:"union_id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	TenantKey string `json:"tenant_key"`
}

func (c *Client) Me(ctx context.Context) (UserInfo, error) {
	var result UserInfo
	err := c.api(ctx, http.MethodGet, "/authen/v1/user_info", nil, &result)
	if err == nil && result.OpenID == "" {
		err = ErrInvalidResponse
	}
	return result, err
}

// AccessToken serializes refresh and persists rotated refresh tokens before use.
// An in-flight refresh has a bounded independent context to finish persistence
// even when a waiting HTTP/document operation is canceled.
func (c *Client) AccessToken(ctx context.Context) (string, error) {
	if err := c.lock(ctx); err != nil {
		return "", err
	}
	defer c.unlock()
	if c.refreshLocker != nil {
		unlock, err := c.refreshLocker.Lock(ctx)
		if err != nil {
			return "", err
		}
		defer unlock()
	}
	if c.pending != nil {
		if err := c.persist(ctx, *c.pending); err != nil {
			return "", err
		}
	}
	token, err := c.store.Load(ctx)
	if errors.Is(err, ErrNoToken) {
		return "", ErrLoginRequired
	}
	if err != nil {
		return "", err
	}
	if token.AppID != c.appID || token.Issuer != c.accountsURL || token.APIBaseURL != c.baseURL || token.OpenID == "" {
		return "", errors.New("feishu/user: stored session does not match this app, endpoint or user")
	}
	if !strings.EqualFold(token.TokenType, "Bearer") {
		return "", ErrUnsupportedToken
	}
	if err := c.checkScopes(token.Scope); err != nil {
		return "", err
	}
	now := c.now()
	skew := 30 * time.Second
	if lifetime := token.ExpiresAt.Sub(token.ObtainedAt); !token.ObtainedAt.IsZero() && lifetime > 0 && lifetime/10 < skew {
		skew = lifetime / 10
	}
	if token.AccessToken != "" && token.ExpiresAt.After(now.Add(skew)) {
		return token.AccessToken, nil
	}
	if token.RefreshToken == "" {
		if token.AccessToken != "" && token.ExpiresAt.After(now) {
			return token.AccessToken, nil
		}
		return "", ErrLoginRequired
	}
	if !token.RefreshExpiresAt.IsZero() && !token.RefreshExpiresAt.After(now) {
		return "", ErrLoginRequired
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.timeout)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"grant_type": "refresh_token", "refresh_token": token.RefreshToken, "client_id": c.appID, "client_secret": c.appSecret})
	issued := c.now()
	data, status, _, err := c.send(refreshCtx, http.MethodPost, c.accountsURL+"/oauth/v3/token", "application/json", body, "")
	if err != nil {
		return "", err
	}
	if err := oauthFailure(data, status); err != nil {
		var oauthErr *OAuthError
		if errors.As(err, &oauthErr) && (oauthErr.ErrorCode == "invalid_grant" || oauthErr.ErrorCode == "invalid_token") {
			return "", errors.Join(ErrLoginRequired, err)
		}
		return "", err
	}
	updated, err := c.decodeToken(data, token, issued)
	if err != nil {
		return "", err
	}
	if err := c.persist(refreshCtx, updated); err != nil {
		return "", err
	}
	if err := c.checkScopes(updated.Scope); err != nil {
		return "", err
	}
	return updated.AccessToken, nil
}

// Logout clears this local session. It does not revoke authorization at Feishu.
func (c *Client) Logout(ctx context.Context) error {
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer c.unlock()
	if err := c.store.Delete(ctx); err != nil {
		return err
	}
	c.pending = nil
	return nil
}

func (c *Client) persist(ctx context.Context, token Token) error {
	c.pending = &token
	if err := c.store.Save(ctx, token); err != nil {
		return err
	}
	c.pending = nil
	return nil
}

func (c *Client) decodeToken(data []byte, previous Token, issued time.Time) (Token, error) {
	var wire struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
		Scope            string `json:"scope"`
		TokenType        string `json:"token_type"`
	}
	if json.Unmarshal(data, &wire) != nil || wire.AccessToken == "" || !validSeconds(wire.ExpiresIn) || wire.RefreshExpiresIn < 0 || wire.RefreshExpiresIn > math.MaxInt64/int64(time.Second) {
		return Token{}, ErrInvalidResponse
	}
	if wire.TokenType == "" {
		wire.TokenType = "Bearer"
	}
	if !strings.EqualFold(wire.TokenType, "Bearer") {
		return Token{}, ErrUnsupportedToken
	}
	t := previous
	t.AppID = c.appID
	t.Issuer = c.accountsURL
	t.APIBaseURL = c.baseURL
	t.AccessToken = wire.AccessToken
	t.TokenType = "Bearer"
	t.ObtainedAt = issued
	t.ExpiresAt = issued.Add(time.Duration(wire.ExpiresIn) * time.Second)
	if wire.Scope != "" {
		t.Scope = wire.Scope
	}
	if wire.RefreshToken != "" {
		t.RefreshToken = wire.RefreshToken
		t.RefreshExpiresAt = time.Time{}
		if wire.RefreshExpiresIn > 0 {
			t.RefreshExpiresAt = issued.Add(time.Duration(wire.RefreshExpiresIn) * time.Second)
		}
	}
	return t, nil
}

func validSeconds(seconds int64) bool {
	return seconds > 0 && seconds <= math.MaxInt64/int64(time.Second)
}

func oauthFailure(data []byte, status int) error {
	var result struct {
		Code        int    `json:"code"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
		Message     string `json:"msg"`
	}
	if json.Unmarshal(data, &result) != nil {
		if status < 200 || status >= 300 {
			return &OAuthError{StatusCode: status}
		}
		return ErrInvalidResponse
	}
	if status < 200 || status >= 300 || result.Error != "" || result.Code != 0 {
		if result.Description == "" {
			result.Description = result.Message
		}
		return &OAuthError{StatusCode: status, Code: result.Code, ErrorCode: result.Error, Description: result.Description}
	}
	return nil
}
