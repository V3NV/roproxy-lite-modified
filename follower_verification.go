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

type followingPage struct {
	NextPageCursor *string `json:"nextPageCursor"`
	Data           []struct {
		ID int64 `json:"id"`
	} `json:"data"`
}

type verificationResult struct { allowed bool; expires time.Time }
var playerCache = struct { sync.RWMutex; results map[int64]verificationResult }{results: make(map[int64]verificationResult)}

func followedUserIDs() []int64 {
	value := strings.TrimSpace(os.Getenv("FOLLOWED_USER_IDS"))
	if value == "" { return defaultFollowedUserIDs }
	var ids []int64
	for _, item := range strings.Split(value, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(item), 10, 64); err == nil && id > 0 { ids = append(ids, id) }
	}
	if len(ids) == 0 { return defaultFollowedUserIDs }
	return ids
}

func envPositiveInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 { return fallback }
	return value
}

func handleFollowerVerification(ctx *fasthttp.RequestCtx) {
	userID, err := strconv.ParseInt(string(ctx.QueryArgs().Peek("userId")), 10, 64)
	if err != nil || userID <= 0 { writeVerificationResponse(ctx, 400, false, false, "userId must be a positive integer"); return }
	for _, requiredID := range followedUserIDs() { if userID == requiredID { writeVerificationResponse(ctx, 200, true, false, ""); return } }
	playerCache.RLock(); cached, found := playerCache.results[userID]; playerCache.RUnlock()
	if found && cached.expires.After(time.Now()) { writeVerificationResponse(ctx, 200, cached.allowed, true, ""); return }
	allowed, err := playerFollowsRequiredAccount(userID)
	if err != nil { log.Printf("Follower verification failed for %d: %v", userID, err); writeVerificationResponse(ctx, 503, false, false, "Follower verification is temporarily unavailable."); return }
	playerCache.Lock()
	playerCache.results[userID] = verificationResult{allowed: allowed, expires: time.Now().Add(time.Duration(envPositiveInt("FOLLOWING_CACHE_MINUTES", 60)) * time.Minute)}
	playerCache.Unlock()
	writeVerificationResponse(ctx, 200, allowed, false, "")
}

func playerFollowsRequiredAccount(userID int64) (bool, error) {
	required := make(map[int64]struct{})
	for _, id := range followedUserIDs() { required[id] = struct{}{} }
	cursor := ""
	for {
		endpoint := fmt.Sprintf("https://friends.roblox.com/v1/users/%d/followings?limit=100", userID)
		if cursor != "" { endpoint += "&cursor=" + url.QueryEscape(cursor) }
		body, err := fetchRobloxPage(endpoint)
		if err != nil { return false, err }
		var response followingPage
		if err := json.Unmarshal(body, &response); err != nil { return false, fmt.Errorf("decode following response: %w", err) }
		for _, following := range response.Data { if _, ok := required[following.ID]; ok { return true, nil } }
		if response.NextPageCursor == nil || *response.NextPageCursor == "" { return false, nil }
		cursor = *response.NextPageCursor
		time.Sleep(time.Second)
	}
}

func fetchRobloxPage(endpoint string) ([]byte, error) {
	for attempt := 1; attempt <= 8; attempt++ {
		req, resp := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
		req.Header.SetMethod(fasthttp.MethodGet); req.Header.Set("User-Agent", "FollowerVerification/1.0"); applyRobloxCookie(req); req.SetRequestURI(endpoint)
		err := client.Do(req, resp)
		status, retryAfter := resp.StatusCode(), string(resp.Header.Peek("Retry-After"))
		body := append([]byte(nil), resp.Body()...)
		fasthttp.ReleaseRequest(req); fasthttp.ReleaseResponse(resp)
		if err == nil && status >= 200 && status < 300 { return body, nil }
		if err != nil { return nil, err }
		if status != 429 || attempt == 8 { return nil, fmt.Errorf("Roblox returned HTTP %d", status) }
		seconds, _ := strconv.Atoi(retryAfter); if seconds <= 0 { seconds = attempt * 5 }
		log.Printf("Roblox rate-limited verification; retrying in %ds (attempt %d of 8)", seconds, attempt)
		time.Sleep(time.Duration(seconds) * time.Second)
	}
	return nil, fmt.Errorf("verification request retry limit reached")
}

func writeVerificationResponse(ctx *fasthttp.RequestCtx, status int, allowed, cached bool, message string) {
	ctx.SetContentType("application/json"); ctx.SetStatusCode(status)
	if message != "" { ctx.SetBodyString(fmt.Sprintf(`{"allowed":%t,"cached":%t,"error":%q}`, allowed, cached, message)); return }
	ctx.SetBodyString(fmt.Sprintf(`{"allowed":%t,"cached":%t}`, allowed, cached))
}
