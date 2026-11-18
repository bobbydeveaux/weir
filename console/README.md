# Weir console

The web console for Weir: workflows and their diagrams, runs as they happen, human tasks, usage and spend.

It is a SvelteKit single-page app (Svelte 5, TypeScript, `adapter-static`) with no UI library and no CSS framework. It only reads and writes through the Weir HTTP API.

## Run it

```sh
npm install
WEIR_API_URL=http://localhost:8080 npm run dev
```

The browser always calls the relative path `/v1/...`. The dev server (and `npm run preview`) proxies `/v1` to `WEIR_API_URL`, which defaults to `http://localhost:8080`.

Pick a workspace in the top bar and type your name into "Acting as": the API needs `X-Weir-Actor` on every write (starting a run, resuming one, deciding a task, creating a workspace). Both are remembered in the browser.

## Build

```sh
npm run build     # static files in build/
npm run check     # svelte-check
```

`build/` is plain static files with `index.html` as the fallback for every route. The Weir server serves it itself when `WEIR_CONSOLE_DIR` points at it, and the server image is built that way. Otherwise serve it from the same origin as the API, behind a reverse proxy that sends `/v1` to the API and everything else to `build/` (falling back to `index.html`).

If the API is not at `/v1`, say where at build time: `VITE_WEIR_API_BASE=/api/v1 npm run build`.

## Layout

- `src/lib/api.ts`: the only module that calls the API; adds the workspace and actor headers.
- `src/lib/types.ts`: the API's shapes, matching `engine/views.go` and the Go structs.
- `src/lib/graph.ts`: lays a definition's steps out as the diagram.
- `src/lib/query.svelte.ts`: loading, error and polling for one read.
- `src/lib/components/`: the diagram, step panels, JSON and YAML views, task card, chart.
- `src/routes/`: one folder per page.
- `src/app.css`: design tokens (shared with the brochure site) and the few global classes.
