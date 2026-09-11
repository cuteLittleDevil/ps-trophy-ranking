// Package psn fetches a public PSN trophy summary by Online ID.
//
// Sony does not offer official OAuth for third-party trophy apps. The client
// holds an operator NPSSO cookie, exchanges it for an access token, resolves
// Online ID → accountId (me profile, then exact universalSearch, then legacy
// profile2), and GET trophy/v1/users/{accountId}/trophySummary.
//
// Tokens and NPSSO never appear in errors, logs, or EnableDump output.
package psn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
)

// Unofficial Android PSN client credentials. Needed to exchange NPSSO for a token.
const (
	clientID    = "09515159-7237-4370-9b40-3806e67c0891"
	redirectURI = "com.scee.psxandroid.scecompcall://redirect"
	scope       = "psn:mobile.v2.core psn:clientapp"
	tokenBasic  = "Basic MDk1MTUxNTktNzIzNy00MzcwLTliNDAtMzgwNmU2N2MwODkxOnVjUGprYTV0bnRCMktxc1A="
	httpTimeout = 10 * time.Second
	expirySkew  = time.Minute
)

// Endpoints are unofficial PSN URLs. Tests replace them with httptest.
type Endpoints struct {
	Auth   string
	Search string
	User   string
	Legacy string
	Trophy string
}

func DefaultEndpoints() Endpoints {
	return Endpoints{
		Auth:   "https://ca.account.sony.com/api/authz/v3/oauth",
		Search: "https://m.np.playstation.com/api/search",
		User:   "https://m.np.playstation.com/api/userProfile/v1/internal/users",
		Legacy: "https://us-prof.np.community.playstation.net/userProfile/v1/users",
		Trophy: "https://m.np.playstation.com/api/trophy",
	}
}

// Counts is Sony earnedTrophies. Score is computed locally, not from trophyPoint.
type Counts struct {
	Bronze   int `json:"bronze"`
	Silver   int `json:"silver"`
	Gold     int `json:"gold"`
	Platinum int `json:"platinum"`
}

// Summary is the ranking-facing trophy snapshot for one Online ID.
type Summary struct {
	OnlineID  string
	DisplayID string
	AvatarURL string
	AccountID string
	Counts
	Score int
	// TrophyJSON is the complete Sony trophySummary body (not OAuth).
	TrophyJSON json.RawMessage
}

// CallDump is one upstream HTTP response, excluding OAuth token exchange.
type CallDump struct {
	Method string
	URL    string
	Status int
	Body   []byte
}

type Client struct {
	npsso string
	ep    Endpoints
	http  *resty.Client

	mu           sync.Mutex
	accessToken  string
	refreshToken string
	accessExpiry time.Time

	dumpMu sync.Mutex
	dumpOn bool
	dumps  []CallDump
}

func New(npsso string) *Client {
	return NewWithEndpoints(npsso, DefaultEndpoints())
}

func NewWithEndpoints(npsso string, ep Endpoints) *Client {
	httpClient := &http.Client{
		Timeout: httpTimeout,
		// /authorize returns 302 with code= in Location. Following it drops the code.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	rc := resty.NewWithClient(httpClient)
	rc.SetTimeout(httpTimeout)
	rc.SetRetryCount(0)
	rc.SetHeader("Accept", "application/json")
	rc.SetDebug(false)
	rc.SetDisableWarn(true)
	rc.SetLogger(discardLogger{})
	c := &Client{npsso: strings.TrimSpace(npsso), ep: ep, http: rc}
	rc.OnAfterResponse(func(_ *resty.Client, resp *resty.Response) error {
		c.recordDump(resp)
		return nil
	})
	return c
}

// EnableDump records non-auth response bodies for CLI inspection.
func (c *Client) EnableDump() {
	c.dumpMu.Lock()
	defer c.dumpMu.Unlock()
	c.dumpOn = true
	c.dumps = nil
}

func (c *Client) Dumps() []CallDump {
	c.dumpMu.Lock()
	defer c.dumpMu.Unlock()
	out := make([]CallDump, len(c.dumps))
	copy(out, c.dumps)
	return out
}

func (c *Client) recordDump(resp *resty.Response) {
	if resp == nil || resp.Request == nil {
		return
	}
	c.dumpMu.Lock()
	defer c.dumpMu.Unlock()
	if !c.dumpOn {
		return
	}
	rawURL := resp.Request.URL
	if isAuthURL(rawURL) {
		return
	}
	body := append([]byte(nil), resp.Body()...)
	c.dumps = append(c.dumps, CallDump{
		Method: resp.Request.Method,
		URL:    rawURL,
		Status: resp.StatusCode(),
		Body:   body,
	})
}

// isAuthURL skips OAuth authorize/token bodies so dumps cannot leak tokens.
func isAuthURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	path := rawURL
	if err == nil {
		path = u.Path
	}
	return strings.Contains(path, "/oauth") ||
		strings.Contains(path, "/authz") ||
		strings.HasSuffix(path, "/token") ||
		strings.HasSuffix(path, "/authorize")
}

type discardLogger struct{}

func (discardLogger) Errorf(string, ...any) {}
func (discardLogger) Warnf(string, ...any)  {}
func (discardLogger) Debugf(string, ...any) {}

// Lookup resolves onlineID and returns earned trophy counts.
// Order: token → accountId → trophySummary. Empty NPSSO does not hit the network.
func (c *Client) Lookup(ctx context.Context, onlineID string) (*Summary, error) {
	onlineID = strings.TrimSpace(onlineID)
	if onlineID == "" {
		return nil, kindErr(KindNotFound)
	}
	if c.npsso == "" {
		return nil, kindErr(KindNoCredentials)
	}
	token, err := c.ensureToken(ctx)
	if err != nil {
		return nil, err
	}

	resolved, err := c.resolveUser(ctx, token, onlineID)
	if err != nil {
		return nil, err
	}

	counts, err := c.trophySummary(ctx, token, resolved.accountID)
	if err != nil {
		return nil, err
	}
	if counts.earned.Bronze < 0 || counts.earned.Silver < 0 || counts.earned.Gold < 0 || counts.earned.Platinum < 0 {
		return nil, kindErr(KindUpstream)
	}

	return &Summary{
		OnlineID:   strings.ToLower(resolved.displayID),
		DisplayID:  resolved.displayID,
		AvatarURL:  resolved.avatarURL,
		AccountID:  resolved.accountID,
		Counts:     counts.earned,
		Score:      counts.earned.Bronze*15 + counts.earned.Silver*30 + counts.earned.Gold*90 + counts.earned.Platinum*300,
		TrophyJSON: counts.raw,
	}, nil
}

type resolvedUser struct {
	accountID string
	displayID string
	avatarURL string
}

// resolveUser maps Online ID to numeric accountId.
// Live search often returns zero hits; profile2 is the working fallback.
func (c *Client) resolveUser(ctx context.Context, token, onlineID string) (resolvedUser, error) {
	me, err := c.meProfile(ctx, token)
	if err != nil {
		return resolvedUser{}, err
	}
	if me != nil && strings.EqualFold(me.displayID, onlineID) {
		return *me, nil
	}

	found, err := c.searchExact(ctx, token, onlineID)
	if err != nil {
		return resolvedUser{}, err
	}
	if found != nil {
		return *found, nil
	}

	legacy, err := c.legacyProfile(ctx, token, onlineID)
	if err != nil {
		return resolvedUser{}, err
	}
	if legacy != nil {
		return *legacy, nil
	}
	return resolvedUser{}, kindErr(KindNotFound)
}

// ensureToken reuses a still-valid access token, then refresh, then NPSSO login.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Add(expirySkew).Before(c.accessExpiry) {
		return c.accessToken, nil
	}
	if c.refreshToken != "" {
		if err := c.refreshLocked(ctx); err == nil {
			return c.accessToken, nil
		}
	}
	if err := c.loginLocked(ctx); err != nil {
		return "", err
	}
	return c.accessToken, nil
}

func (c *Client) loginLocked(ctx context.Context) error {
	code, err := c.exchangeNpsso(ctx)
	if err != nil {
		return err
	}
	return c.exchangeCode(ctx, code)
}

// exchangeNpsso sends the NPSSO cookie to /authorize and reads code from Location.
func (c *Client) exchangeNpsso(ctx context.Context) (string, error) {
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("Cookie", "npsso="+c.npsso).
		SetQueryParams(map[string]string{
			"access_type":   "offline",
			"client_id":     clientID,
			"redirect_uri":  redirectURI,
			"response_type": "code",
			"scope":         scope,
		}).
		Get(joinURL(c.ep.Auth, "/authorize"))
	if err != nil {
		return "", kindErr(KindUpstream)
	}
	code := parseAccessCode(resp.Header().Get("Location"))
	if code == "" {
		return "", kindErr(KindInvalidCredentials)
	}
	return code, nil
}

func parseAccessCode(location string) string {
	if location == "" {
		return ""
	}
	if i := strings.Index(location, "code="); i >= 0 {
		rest := location[i+len("code="):]
		if j := strings.IndexAny(rest, "&?#"); j >= 0 {
			rest = rest[:j]
		}
		code, err := url.QueryUnescape(rest)
		if err != nil {
			return rest
		}
		return code
	}
	u, err := url.Parse(location)
	if err != nil {
		return ""
	}
	return u.Query().Get("code")
}

type tokenResponse struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
}

func (c *Client) exchangeCode(ctx context.Context, code string) error {
	return c.postToken(ctx, map[string]string{
		"code":         code,
		"redirect_uri": redirectURI,
		"grant_type":   "authorization_code",
		"token_format": "jwt",
	})
}

func (c *Client) refreshLocked(ctx context.Context) error {
	return c.postToken(ctx, map[string]string{
		"refresh_token": c.refreshToken,
		"grant_type":    "refresh_token",
		"token_format":  "jwt",
		"scope":         scope,
	})
}

func (c *Client) postToken(ctx context.Context, form map[string]string) error {
	var tok tokenResponse
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("Authorization", tokenBasic).
		SetFormData(form).
		SetResult(&tok).
		Post(joinURL(c.ep.Auth, "/token"))
	if err != nil {
		return kindErr(KindUpstream)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return kindErr(KindInvalidCredentials)
	}
	if tok.AccessToken == "" {
		if err := json.Unmarshal(resp.Body(), &tok); err != nil || tok.AccessToken == "" {
			return kindErr(KindUpstream)
		}
	}
	c.accessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.refreshToken = tok.RefreshToken
	}
	expires := tok.ExpiresIn
	if expires <= 0 {
		expires = 3600
	}
	c.accessExpiry = time.Now().Add(time.Duration(expires) * time.Second)
	return nil
}

type meProfileJSON struct {
	OnlineID string `json:"onlineId"`
	Avatars  []struct {
		Size string `json:"size"`
		URL  string `json:"url"`
	} `json:"avatars"`
}

type namedAvatar struct {
	size string
	url  string
}

// meProfile is a shortcut when looking up the authenticating account.
// GET .../users/me/profiles expects a numeric accountId, so "me" is usually 400.
func (c *Client) meProfile(ctx context.Context, token string) (*resolvedUser, error) {
	var body meProfileJSON
	resp, err := c.authedGet(ctx, token, joinURL(c.ep.User, "/me/profiles"), &body, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() == http.StatusBadRequest || resp.StatusCode() == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, kindErr(KindInvalidCredentials)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, kindErr(KindUpstream)
	}
	if body.OnlineID == "" {
		if err := json.Unmarshal(resp.Body(), &body); err != nil {
			return nil, kindErr(KindUpstream)
		}
	}
	if body.OnlineID == "" {
		return nil, nil
	}
	avatars := make([]namedAvatar, 0, len(body.Avatars))
	for _, a := range body.Avatars {
		avatars = append(avatars, namedAvatar{size: a.Size, url: a.URL})
	}
	return &resolvedUser{accountID: "me", displayID: body.OnlineID, avatarURL: pickAvatar(avatars)}, nil
}

type searchRequest struct {
	SearchTerm     string `json:"searchTerm"`
	DomainRequests []struct {
		Domain string `json:"domain"`
	} `json:"domainRequests"`
}

type searchResponse struct {
	DomainResponses []struct {
		Results []struct {
			SocialMetadata struct {
				AccountID string `json:"accountId"`
				OnlineID  string `json:"onlineId"`
				AvatarURL string `json:"avatarUrl"`
			} `json:"socialMetadata"`
		} `json:"results"`
	} `json:"domainResponses"`
}

// searchExact requires an exact Online ID match (ignore case). Similar names are ignored.
func (c *Client) searchExact(ctx context.Context, token, onlineID string) (*resolvedUser, error) {
	payload := searchRequest{
		SearchTerm: onlineID,
		DomainRequests: []struct {
			Domain string `json:"domain"`
		}{{Domain: "SocialAllAccounts"}},
	}
	var body searchResponse
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(token).
		SetHeader("Content-Type", "application/json").
		SetBody(payload).
		SetResult(&body).
		Post(joinURL(c.ep.Search, "/v1/universalSearch"))
	if err != nil {
		return nil, kindErr(KindUpstream)
	}
	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, kindErr(KindInvalidCredentials)
	}
	if resp.StatusCode() == http.StatusTooManyRequests {
		return nil, kindErr(KindUpstream)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, kindErr(KindUpstream)
	}
	if len(body.DomainResponses) == 0 {
		if err := json.Unmarshal(resp.Body(), &body); err != nil {
			return nil, kindErr(KindUpstream)
		}
	}
	for _, domain := range body.DomainResponses {
		for _, result := range domain.Results {
			meta := result.SocialMetadata
			if meta.AccountID == "" || !strings.EqualFold(meta.OnlineID, onlineID) {
				continue
			}
			return &resolvedUser{
				accountID: meta.AccountID,
				displayID: meta.OnlineID,
				avatarURL: meta.AvatarURL,
			}, nil
		}
	}
	return nil, nil
}

type legacyProfileResponse struct {
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Profile struct {
		OnlineID   string `json:"onlineId"`
		AccountID  string `json:"accountId"`
		AvatarURLs []struct {
			Size      string `json:"size"`
			AvatarURL string `json:"avatarUrl"`
		} `json:"avatarUrls"`
	} `json:"profile"`
}

// legacyProfile is the reliable Online ID → accountId lookup when search is empty.
func (c *Client) legacyProfile(ctx context.Context, token, onlineID string) (*resolvedUser, error) {
	fields := "npId,onlineId,accountId,avatarUrls,trophySummary(@default,level,progress,earnedTrophies)"
	u := joinURL(c.ep.Legacy, url.PathEscape(onlineID)+"/profile2")
	var body legacyProfileResponse
	resp, err := c.authedGet(ctx, token, u, &body, map[string]string{"fields": fields})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, kindErr(KindInvalidCredentials)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, kindErr(KindUpstream)
	}
	if body.Profile.AccountID == "" && body.Error == nil {
		if err := json.Unmarshal(resp.Body(), &body); err != nil {
			return nil, kindErr(KindUpstream)
		}
	}
	if body.Error != nil {
		return nil, nil
	}
	if body.Profile.AccountID == "" {
		return nil, nil
	}
	avatars := make([]namedAvatar, 0, len(body.Profile.AvatarURLs))
	for _, a := range body.Profile.AvatarURLs {
		avatars = append(avatars, namedAvatar{size: a.Size, url: a.AvatarURL})
	}
	display := body.Profile.OnlineID
	if display == "" {
		display = onlineID
	}
	return &resolvedUser{
		accountID: body.Profile.AccountID,
		displayID: display,
		avatarURL: pickAvatar(avatars),
	}, nil
}

type trophySummaryResponse struct {
	AccountID            string `json:"accountId"`
	TrophyLevel          int    `json:"trophyLevel"`
	TrophyPoint          int    `json:"trophyPoint"`
	TrophyLevelBasePoint int    `json:"trophyLevelBasePoint"`
	TrophyLevelNextPoint int    `json:"trophyLevelNextPoint"`
	Progress             int    `json:"progress"`
	Tier                 int    `json:"tier"`
	EarnedTrophies       Counts `json:"earnedTrophies"`
	Error                *struct {
		Code int `json:"code"`
	} `json:"error"`
}

type trophyResult struct {
	earned Counts
	raw    json.RawMessage
}

// trophySummary is the only trophy endpoint used for ranking.
// 403 means the list is private. The raw body is kept for --raw / tests.
func (c *Client) trophySummary(ctx context.Context, token, accountID string) (trophyResult, error) {
	var body trophySummaryResponse
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(token).
		SetPathParam("accountId", accountID).
		SetResult(&body).
		Get(joinURL(c.ep.Trophy, "/v1/users/{accountId}/trophySummary"))
	if err != nil {
		return trophyResult{}, kindErr(KindUpstream)
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized:
		return trophyResult{}, kindErr(KindInvalidCredentials)
	case http.StatusForbidden:
		return trophyResult{}, kindErr(KindPrivate)
	case http.StatusNotFound:
		return trophyResult{}, kindErr(KindNotFound)
	case http.StatusTooManyRequests:
		return trophyResult{}, kindErr(KindUpstream)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return trophyResult{}, kindErr(KindUpstream)
	}
	raw := append([]byte(nil), resp.Body()...)
	if body.EarnedTrophies == (Counts{}) && body.Error == nil {
		if err := json.Unmarshal(raw, &body); err != nil {
			return trophyResult{}, kindErr(KindUpstream)
		}
	}
	if body.Error != nil {
		return trophyResult{}, kindErr(KindPrivate)
	}
	return trophyResult{earned: body.EarnedTrophies, raw: json.RawMessage(raw)}, nil
}

func (c *Client) authedGet(ctx context.Context, token, rawURL string, result any, query map[string]string) (*resty.Response, error) {
	req := c.http.R().SetContext(ctx).SetAuthToken(token)
	if result != nil {
		req.SetResult(result)
	}
	if len(query) > 0 {
		req.SetQueryParams(query)
	}
	resp, err := req.Get(rawURL)
	if err != nil {
		return nil, kindErr(KindUpstream)
	}
	return resp, nil
}

func pickAvatar(avatars []namedAvatar) string {
	fallback := ""
	for _, a := range avatars {
		if a.url == "" {
			continue
		}
		fallback = a.url
		if strings.EqualFold(a.size, "xl") {
			return a.url
		}
	}
	return fallback
}

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
