package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

var defaultFollowedUserIDs = []int64{5489385758, 768997053}

type followerPage struct {
	NextPageCursor *string `json:"nextPageCursor"`
	Data           []struct {
		ID int64 `json:"id"`
	} `json:"data"`
}

type followerVerificationCache struct {
	sync.RWMutex
	followerIDs map[int64]struct{}
	ready       bool
	refreshing  bool
	updatedAt   time.Time
}

var verificationCache = followerVerificationCache{
	followerIDs: make(map[int64]struct{}),
}

func followedUserIDs() []int64 {
	value := strings.TrimSpace(os.Getenv("FOLLOWED_USER_IDS"))
	if value == "" {
		return defaultFollowedUserIDs
	}

	var ids []int64
	for _, item := range strings.Split(value, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(item), 10, 64)
		if err == nil && id > 0 {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		log.Println("FOLLOWED_USER_IDS was invalid; using the configured defaults")
		return defaultFollowedUserIDs
	}
	return ids
}

func envPositiveInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func startFollowerCache() {
	refreshInterval := time.Duration(envPositiveInt("FOLLOWER_REFRESH_MINUTES", 30)) * time.Minute
	go func() {
		for {
			refreshFollowerCache()
			time.Sleep(refreshInterval)
		}
	}()
}

func refreshFollowerCache() {
	verificationCache.Lock()
	if verificationCache.refreshing {
		verificationCache.Unlock()
		return
	}
	verificationCache.refreshing = true
	verificationCache.Unlock()

	defer func() {
		verificationCache.Lock()
		verificationCache.refreshing = false
		verificationCache.Unlock()
	}()

	ids := followedUserIDs()
	newFollowerIDs := make(map[int64]struct{})
	for _, userID := range ids {
		followerIDs, err := fetchAllFollowerIDs(userID)
		if err != nil {
			log.Printf("Follower cache refresh failed for %d: %v", userID, err)
			return // Keep the previous complete cache if a refresh fails.
		}

		for followerID := range followerIDs {
			newFollowerIDs[followerID] = struct{}{}
		}
	}

	verificationCache.Lock()
	verificationCache.followerIDs = newFollowerIDs
	verificationCache.ready = true
	verificationCache.updatedAt = time.Now()
	verificationCache.Unlock()
	log.Printf("Follower cache refreshed with %d follower IDs", len(newFollowerIDs))
}

func fetchAllFollowerIDs(userID int64) (map[int64]struct{}, error) {
	followerIDs := make(map[int64]struct{})
	cursor := ""
	page := 1
	delay := time.Duration(envPositiveInt("FOLLOWER_REQUEST_DELAY_MS", 1000)) * time.Millisecond

	for {
		endpoint := fmt.Sprintf("https://friends.roblox.com/v1/users/%d/followers?limit=100", userID)
		if cursor != "" {
			endpoint += "&cursor=" + url.QueryEscape(cursor)
		}

		body, err := fetchFollowerPage(endpoint)
		if err != nil {
			return nil, err
		}

		var response followerPage
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("decode page %d: %w", page, err)
		}

		for _, follower := range response.Data {
			followerIDs[follower.ID] = struct{}{}
		}

		if response.NextPageCursor == nil || *response.NextPageCursor == "" {
			return followerIDs, nil
		}

		cursor = *response.NextPageCursor
		page++
		time.Sleep(delay)
	}
}

func fetchFollowerPage(endpoint string) ([]byte, error) {
	for attempt := 1; attempt <= 5; attempt++ {
		req := fasthttp.AcquireRequest()
		resp := fasthttp.AcquireResponse()
		req.Header.SetMethod(fasthttp.MethodGet)
		req.Header.Set("User-Agent", "FollowerVerificationCache/1.0")
		applyRobloxCookie(req)
		req.SetRequestURI(endpoint)

		err := client.Do(req, resp)
		statusCode := resp.StatusCode()
		body := append([]byte(nil), resp.Body()...)
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(resp)

		if err == nil && statusCode >= 200 && statusCode < 300 {
			return body, nil
		}

		if statusCode != fasthttp.StatusTooManyRequests || attempt == 5 {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("Roblox returned HTTP %d", statusCode)
		}

		time.Sleep(time.Duration(attempt) * time.Second)
	}

	return nil, fmt.Errorf("follower request retry limit reached")
}

func handleFollowerVerification(ctx *fasthttp.RequestCtx) {
	userID, err := strconv.ParseInt(string(ctx.QueryArgs().Peek("userId")), 10, 64)
	if err != nil || userID <= 0 {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		ctx.SetBodyString(`{"allowed":false,"error":"userId must be a positive integer"}`)
		return
	}

	verificationCache.RLock()
	ready := verificationCache.ready
	updatedAt := verificationCache.updatedAt
	_, followsRequiredAccount := verificationCache.followerIDs[userID]
	verificationCache.RUnlock()

	if !ready {
		ctx.SetContentType("application/json")
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"allowed":false,"ready":false,"message":"Follower cache is warming."}`)
		return
	}

	allowed := followsRequiredAccount
	for _, followedUserID := range followedUserIDs() {
		if userID == followedUserID {
			allowed = true
			break
		}
	}

	ctx.SetContentType("application/json")
	ctx.SetBodyString(fmt.Sprintf(`{"allowed":%t,"ready":true,"cacheAgeSeconds":%d}`,
		allowed,
		int(time.Since(updatedAt).Seconds()),
	))
}
