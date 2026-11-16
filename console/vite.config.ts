import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// The browser only ever calls the relative path /v1. In dev and preview that
// path is proxied to the Weir API; in production, serve build/ behind the same
// origin as the API (or put a reverse proxy in front of both).
const api = process.env.WEIR_API_URL ?? 'http://localhost:8080';
const proxy = { '/v1': { target: api, changeOrigin: true } };

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			// A single-page app: plain static files, every route served by index.html.
			adapter: adapter({ pages: 'build', assets: 'build', fallback: 'index.html' })
		})
	],
	define: {
		// Shown in the "cannot reach the API" banner while developing.
		__WEIR_PROXY_TARGET__: JSON.stringify(api)
	},
	server: { proxy },
	preview: { proxy }
});
