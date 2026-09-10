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

type Counts struct {
	Bronze   int `json:"bronze"`
	Silver   int `json:"silver"`
	Gold     int `json:"gold"`
	Platinum int `json:"platinum"`
}

type Summary struct {
	OnlineID  string
	DisplayID string
	AvatarURL string
	AccountID string
	Counts
	Score int
}

type Client struct {
	npsso string
	ep    Endpoints
	http  *resty.Client

	mu           sync.Mutex
	accessToken  string
	refreshToken string
	accessExpiry time.Time
}

func New(npsso string) *Client {
	return NewWithEndpoints(npsso, DefaultEndpoints())
}

func NewWithEndpoints(npsso string, ep Endpoints) *Client {
	httpClient := &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	rc := resty.NewWithClient(httpClient)
	rc.SetTimeout(httpTimeout)
	rc.SetDebug(false)
	rc.SetDisableWarn(true)
	rc.SetLogger(discardLogger{})
	return &Client{npsso: strings.TrimSpace(npsso), ep: ep, http: rc}
}

type discardLogger struct{}

func (discardLogger) Errorf(string, ...any) {}
func (discardLogger) Warnf(string, ...any)  {}
func (discardLogger) Debugf(string, ...any) {}

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
	if counts.Bronze < 0 || counts.Silver < 0 || counts.Gold < 0 || counts.Platinum < 0 {
		return nil, kindErr(KindUpstream)
	}

	return &Summary{
		OnlineID:  strings.ToLower(resolved.displayID),
		DisplayID: resolved.displayID,
		AvatarURL: resolved.avatarURL,
		AccountID: resolved.accountID,
		Counts:    counts,
		Score:     Score(counts.Bronze, counts.Silver, counts.Gold, counts.Platinum),
	}, nil
}

type resolvedUser struct {
	accountID string
	displayID string
	avatarURL string
}

func (c *Client) resolveUser(ctx context.Context, token, onlineID string) (resolvedUser, error) {
	me, err := c.meProfile(ctx, token)
	if err != nil {
		return resolvedUser{}, err
	}
	if strings.EqualFold(me.displayID, onlineID) {
		return resolvedUser{accountID: "me", displayID: me.displayID, avatarURL: me.avatarURL}, nil
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

func (c *Client) exchangeNpsso(ctx context.Context) (string, error) {
	q := url.Values{
		"access_type":   {"offline"},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {scope},
	}
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("Cookie", "npsso="+c.npsso).
		Get(joinURL(c.ep.Auth, "/authorize") + "?" + q.Encode())
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
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("Authorization", tokenBasic).
		SetFormData(form).
		Post(joinURL(c.ep.Auth, "/token"))
	if err != nil {
		return kindErr(KindUpstream)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return kindErr(KindInvalidCredentials)
	}
	var tok tokenResponse
	if err := json.Unmarshal(resp.Body(), &tok); err != nil || tok.AccessToken == "" {
		return kindErr(KindUpstream)
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

func (c *Client) meProfile(ctx context.Context, token string) (resolvedUser, error) {
	resp, err := c.authedGet(ctx, token, joinURL(c.ep.User, "/me/profiles"))
	if err != nil {
		return resolvedUser{}, err
	}
	if resp.StatusCode() == http.StatusUnauthorized {
		return resolvedUser{}, kindErr(KindInvalidCredentials)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return resolvedUser{}, kindErr(KindUpstream)
	}
	var body meProfileJSON
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return resolvedUser{}, kindErr(KindUpstream)
	}
	avatars := make([]namedAvatar, 0, len(body.Avatars))
	for _, a := range body.Avatars {
		avatars = append(avatars, namedAvatar{size: a.Size, url: a.URL})
	}
	return resolvedUser{accountID: "me", displayID: body.OnlineID, avatarURL: pickAvatar(avatars)}, nil
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

func (c *Client) searchExact(ctx context.Context, token, onlineID string) (*resolvedUser, error) {
	payload := searchRequest{
		SearchTerm: onlineID,
		DomainRequests: []struct {
			Domain string `json:"domain"`
		}{{Domain: "SocialAllAccounts"}},
	}
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(token).
		SetHeader("Content-Type", "application/json").
		SetBody(payload).
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
	var body searchResponse
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return nil, kindErr(KindUpstream)
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

func (c *Client) legacyProfile(ctx context.Context, token, onlineID string) (*resolvedUser, error) {
	fields := "npId,onlineId,accountId,avatarUrls,trophySummary(@default,level,progress,earnedTrophies)"
	u := joinURL(c.ep.Legacy, url.PathEscape(onlineID)+"/profile2") + "?fields=" + url.QueryEscape(fields)
	resp, err := c.authedGet(ctx, token, u)
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
	var body legacyProfileResponse
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return nil, kindErr(KindUpstream)
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
	EarnedTrophies Counts `json:"earnedTrophies"`
	Error          *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func (c *Client) trophySummary(ctx context.Context, token, accountID string) (Counts, error) {
	u := joinURL(c.ep.Trophy, "/v1/users/"+url.PathEscape(accountID)+"/trophySummary")
	resp, err := c.authedGet(ctx, token, u)
	if err != nil {
		return Counts{}, err
	}
	switch resp.StatusCode() {
	case http.StatusUnauthorized:
		return Counts{}, kindErr(KindInvalidCredentials)
	case http.StatusForbidden:
		return Counts{}, kindErr(KindPrivate)
	case http.StatusNotFound:
		return Counts{}, kindErr(KindNotFound)
	case http.StatusTooManyRequests:
		return Counts{}, kindErr(KindUpstream)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return Counts{}, kindErr(KindUpstream)
	}
	var body trophySummaryResponse
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return Counts{}, kindErr(KindUpstream)
	}
	if body.Error != nil {
		return Counts{}, kindErr(KindPrivate)
	}
	return body.EarnedTrophies, nil
}

func (c *Client) authedGet(ctx context.Context, token, rawURL string) (*resty.Response, error) {
	resp, err := c.http.R().SetContext(ctx).SetAuthToken(token).Get(rawURL)
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
