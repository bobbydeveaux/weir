## 1. The contract

- [x] 1.1 Add `event` and `when` to an external step, and `resume: event`
- [x] 1.2 Refuse a wait with no event, no rule or no timeout, and `event` or `when` on another resume mode
- [x] 1.3 Let a step that only waits name no connection and make no call

## 2. The engine

- [x] 2.1 Add the `event_waits` table and the `resumed` column (migration 00018)
- [x] 2.2 Record a wait when a step parks, and look back over events received since the run started
- [x] 2.3 Give an arriving event to every step waiting for its type, each in its own transaction
- [x] 2.4 Make a step starting to wait and an event arriving take turns, so that neither order loses the event

## 3. The API and the console

- [x] 3.1 Report what an event resumed from `POST /v1/events`, `GET /v1/events` and the webhook reply
- [x] 3.2 Show what an event resumed, and what a step waits for, in the console

## 4. Finishing

- [x] 4.1 Tests against Postgres, including a signed webhook through the real endpoint
- [x] 4.2 An example, run by a test: `examples/wait-for-merge`
- [x] 4.3 Update the README, the design, the database notes and the to-do list
- [x] 4.4 Update `proof.md` for both capabilities
- [x] 4.5 `make check` and `make spec`
