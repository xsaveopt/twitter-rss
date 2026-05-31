# twitter-rss

**Re-emits a Twitter/X user's tweets as a clean RSS feed — author-tagged per account, backed by a Nitter instance. Handy for tracking when streamers post "going live".**

## Contents

- [How it works](#how-it-works)
- [First-run setup](#first-run-setup)
- [docker-compose](#docker-compose)
- [Configuration](#configuration)
- [Image tags](#image-tags)
- [Environment variables](#environment-variables)

## How it works

twitter-rss is a small HTTP server. When you request `/u/{handle}`, it fetches that account's RSS from a Nitter instance you point it at, then re-emits it as RSS with the Twitter handle set as the feed and per-item author — so your reader groups and attributes items correctly. Results are cached per handle for a configurable TTL so repeated polling doesn't hammer Nitter.

`/combined?users=a,b,c` merges several accounts into one chronological feed, each item prefixed with its source handle. By default Nitter links are rewritten back to `x.com` so opening an item lands on the real tweet.

It keeps no state on disk — the cache is in memory and rebuilt on restart.

## First-run setup

You need a reachable Nitter instance (public instances are mostly dead, so self-hosting one is the reliable path). Point `TWITTER_RSS_NITTER` at it, start the container, then subscribe your reader to `http://host:8080/u/{handle}`.

## docker-compose

```yaml
services:
  twitter-rss:
    image: ghcr.io/sratabix/twitter-rss:latest
    container_name: twitter-rss
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      TWITTER_RSS_NITTER: https://nitter.example.com
      TWITTER_RSS_CACHE_TTL: 5m
      TZ: Europe/Amsterdam
```

```bash
docker compose up -d
```

Then subscribe to `http://host:8080/u/jack` or `http://host:8080/combined?users=jack,nitter,xdevelopers`.

## Configuration

Everything is configured through environment variables (see the table below). The only required one is `TWITTER_RSS_NITTER`. There is no config file and no persistent state.

## Image tags

`latest` for the latest stable release. `1`, `1.2`, `1.2.3` to pin to a major, minor, or patch line. Pre-releases like `1.2.3-rc1` are never tagged `latest`. `dev` tracks the tip of `main`, rebuilt on every commit — the easiest tag for testing without waiting for a release. Images are published to `ghcr.io/sratabix/twitter-rss` and built for `linux/amd64`.

## Environment variables

| Var                         | Default | Purpose                                                      |
| --------------------------- | ------- | ------------------------------------------------------------ |
| `TWITTER_RSS_NITTER`        | —       | **Required.** Base URL of the Nitter instance to fetch from. |
| `TWITTER_RSS_ADDR`          | `:8080` | HTTP listen address.                                         |
| `TWITTER_RSS_CACHE_TTL`     | `5m`    | How long a fetched feed is reused before re-fetching.        |
| `TWITTER_RSS_REWRITE_LINKS` | `true`  | Rewrite Nitter links back to `x.com`.                        |
| `TWITTER_RSS_HTTP_TIMEOUT`  | `15s`   | Per-request timeout when talking to Nitter.                  |
| `TWITTER_RSS_USER_AGENT`    | derived | User-Agent sent to Nitter.                                   |
| `TZ`                        | UTC     | Standard tz name (e.g. `Europe/Amsterdam`).                  |
