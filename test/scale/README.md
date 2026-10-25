# Scale test

Seeds 100,000 issues (five projects of 20,000, with status, sprint and story point history,
comments, links and releases) and times the API and the heaviest pages. Run it before and after
changes to search, boards, reports or metrics.

Against a server with development sign-in (`WFM_DEV_LOGIN=true`) and its own empty database:

```sh
BASE=http://localhost:8082 node test/scale/setup.mjs     # users, projects, sprints, releases (API)
psql "$DATABASE_URL" -f test/scale/seed.sql              # the issues and their history (about a minute)
BASE=http://localhost:8082 RUNS=5 node test/scale/bench.mjs [name-regexp]   # median and worst per endpoint
BASE=http://localhost:8082 node test/scale/ui.mjs        # first paint, idle and DOM size per page
```

`ui.mjs` needs Playwright's Chromium and a server that serves the UI (`make web embed-web build`).

Measured in a development container with 100,020 issues and 361,000 history rows: every
endpoint in `bench.mjs` answers in under 600 ms (most under 150 ms), and the board and backlog
draw in under a second.
