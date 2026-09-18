# twitter-rss

twitter-rss serves the tweets of a Twitter/X account as an RSS feed fetched through Nitter, with the account set as the author.
Requests rotate across the instances in TWITTER_RSS_NITTER, and an instance that fails is skipped for five minutes while the next one answers.

## Running it

Fill in your instances in docker-compose.yml and start it with docker compose up.
Subscribe to /u/{handle} for one account, or to /combined?users={handle},{handle} to merge several accounts into one feed.
Images are published to ghcr.io/xsaveopt/twitter-rss, where latest is the newest release, 1, 1.2 and 1.2.3 pin a version line, and dev follows main.

## Environment variables

| Variable                    | Default               | Purpose                                                       |
| --------------------------- | --------------------- | ------------------------------------------------------------- |
| `TWITTER_RSS_NITTER`        | required              | Comma-separated base URLs of the Nitter instances to use.     |
| `TWITTER_RSS_ADDR`          | `:8080`               | Address the HTTP server listens on.                           |
| `TWITTER_RSS_BASE_PATH`     | none                  | Subpath the feeds are also served under, like /twitter-rss.   |
| `TWITTER_RSS_CACHE_TTL`     | `5m`                  | How long a fetched feed is served before it is fetched again. |
| `TWITTER_RSS_REWRITE_LINKS` | `true`                | Rewrite Nitter links to x.com.                                |
| `TWITTER_RSS_HTTP_TIMEOUT`  | `15s`                 | Timeout for each request to a Nitter instance.                |
| `TWITTER_RSS_USER_AGENT`    | `twitter-rss/version` | User-Agent sent to Nitter.                                    |
| `TZ`                        | `UTC`                 | Time zone name, like Europe/Amsterdam.                        |
