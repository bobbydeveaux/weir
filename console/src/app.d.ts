// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}

	/** Where the dev and preview servers proxy /v1 to. Set in vite.config.ts. */
	const __WEIR_PROXY_TARGET__: string;
}

export {};
