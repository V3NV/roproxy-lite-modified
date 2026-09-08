# RoProxy Lite
A modified version of RoProxy made for self-hosting.

Setup is easy, simply deploy with the button below and configure environment variables. The KEY variable is optional, leave blank to not require an auth key.

[![Deploy](https://www.herokucdn.com/deploy/button.svg)](https://heroku.com/deploy?template=https://github.com/halffalse/roproxy-lite)
[![Deploy on Railway](https://railway.app/button.svg)](https://railway.app/new/template/fV9Lxm)
[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy)

When "KEY" environment variable is populated, a matching "PROXYKEY" header must be present. Requests must be made in the format /subdomain/path. E.g. https://games.roblox.com/docs -> https://roproxytest.heroku.com/games/docs

## Follower verification

`GET /verify-follower?userId=ROBLOX_USER_ID` returns a JSON `{ "allowed": true }` response for either configured account or one of their followers. It uses the same `PROXYKEY` authentication as the proxy.

The service refreshes the two follower lists in the background and keeps one cache for the entire deployment, so player joins never crawl the Roblox follower API. Configure `FOLLOWED_USER_IDS`, `FOLLOWER_REFRESH_MINUTES` (default `30`), and `FOLLOWER_REQUEST_DELAY_MS` (default `1000`) in Railway. Until the first refresh completes, the endpoint returns HTTP `503` with `ready: false`.
