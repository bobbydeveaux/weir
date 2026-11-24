# Proof: console

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| The console is a static page that talks only to the API | Not tested: checked by the type checker and the build only; the API client is `console/src/lib/api.ts` |
| A person signs in by pasting an API key | Not tested: checked by the type checker and the build only; the fields are in `console/src/routes/+layout.svelte`, and the server's side of it (`GET /v1/me`) is proven under access |
| A person chooses the workspace | Not tested: checked by the type checker and the build only; the chooser is in `console/src/routes/+layout.svelte` and the form in `console/src/routes/settings/+page.svelte` |
| The overview shows what ran and what it cost | Not tested: checked by the type checker and the build only; the page is `console/src/routes/+page.svelte` |
| Workflows can be browsed and started | Not tested: checked by the type checker and the build only; the pages are under `console/src/routes/workflows` |
| Runs can be listed and followed | Not tested: checked by the type checker and the build only; the pages are under `console/src/routes/runs` |
| A blocked run can be resumed or cancelled | Not tested: checked by the type checker and the build only; the buttons are in `console/src/routes/runs/[id]/+page.svelte` |
| Tasks can be seen and decided | Not tested: checked by the type checker and the build only; the card is `console/src/lib/components/TaskCard.svelte` |
| Events, templates and connections can be read | Not tested: checked by the type checker and the build only; the pages are `events`, `templates` and `settings` under `console/src/routes` |
| The server can serve the console itself | `api/front_test.go:TestBehindASignInProxy` (served from a stand-in directory; that the image holds the real console is not tested) |
