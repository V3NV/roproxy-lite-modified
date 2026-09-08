package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
)

var timeout, _ = strconv.Atoi(os.Getenv("TIMEOUT"))
var retries, _ = strconv.Atoi(os.Getenv("RETRIES"))
var port = os.Getenv("PORT")

var client *fasthttp.Client

func main() {
	h := requestHandler

	client = &fasthttp.Client{
		ReadTimeout: time.Duration(timeout) * time.Second,
		MaxIdleConnDuration: 60 * time.Second,
	}

	startFollowerCache()

	if err := fasthttp.ListenAndServe(":" + port, h); err != nil {
		log.Fatalf("Error in ListenAndServe: %s", err)
	}
}

func requestHandler(ctx *fasthttp.RequestCtx) {
	if !hasValidProxyKey(ctx) {
		return
	}

	if string(ctx.Path()) == "/verify-follower" {
		handleFollowerVerification(ctx)
		return
	}

	if len(strings.SplitN(string(ctx.Request.Header.RequestURI())[1:], "/", 2)) < 2 {
		ctx.SetStatusCode(400)
		ctx.SetBody([]byte("URL format invalid."))
		return
	}

	response := makeRequest(ctx, 1)

	defer fasthttp.ReleaseResponse(response)

	body := response.Body()
	ctx.SetBody(body)
	ctx.SetStatusCode(response.StatusCode())
	response.Header.VisitAll(func (key, value []byte) {
		ctx.Response.Header.Set(string(key), string(value))
	})
}

func hasValidProxyKey(ctx *fasthttp.RequestCtx) bool {
	val, ok := os.LookupEnv("KEY")
	if !ok || val == "" || string(ctx.Request.Header.Peek("PROXYKEY")) == val {
		return true
	}

	ctx.SetStatusCode(407)
	ctx.SetBody([]byte("Missing or invalid PROXYKEY header."))
	return false
}

func makeRequest(ctx *fasthttp.RequestCtx, attempt int) *fasthttp.Response {
	if attempt > retries {
		resp := fasthttp.AcquireResponse()
		resp.SetBody([]byte("Proxy failed to connect. Please try again."))
		resp.SetStatusCode(500)
		return resp
	}

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod(string(ctx.Method()))

	url := strings.SplitN(
		string(ctx.Request.Header.RequestURI())[1:],
		"/",
		2,
	)

	req.SetRequestURI(
		"https://" + url[0] + ".roblox.com/" + url[1],
	)

	req.SetBody(ctx.Request.Body())

	ctx.Request.Header.VisitAll(func(key, value []byte) {
		req.Header.Set(string(key), string(value))
	})

	robloxCookie := strings.TrimSpace(os.Getenv("ROBLOX_COOKIE"))

	robloxCookie = strings.TrimPrefix(
		robloxCookie,
		".ROBLOSECURITY=",
	)

	robloxCookie = strings.Trim(robloxCookie, `"`)

	if index := strings.Index(robloxCookie, ";"); index != -1 {
		robloxCookie = robloxCookie[:index]
	}

	if robloxCookie != "" {
		req.Header.Del("Cookie")
		req.Header.Set(
			"Cookie",
			".ROBLOSECURITY="+robloxCookie,
		)
	}

	req.Header.Set("User-Agent", "RoProxy")
	req.Header.Del("Roblox-Id")
	req.Header.Del("PROXYKEY")

	resp := fasthttp.AcquireResponse()

	err := client.Do(req, resp)

	if err != nil {
		fasthttp.ReleaseResponse(resp)
		return makeRequest(ctx, attempt+1)
	}

	return resp
}
