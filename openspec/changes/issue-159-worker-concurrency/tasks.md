## 1. Worker

- [ ] 1.1 Add `Concurrency` to `worker.Worker`, defaulting to 1
- [ ] 1.2 Split `RunOnce` into claiming a step and running it, with the heartbeat kept per step
- [ ] 1.3 Make `Run` take a slot before each claim, run each step in its own goroutine, keep sweeping while every slot is busy, and on an error stop claiming, wait for the steps it holds, and return the first error

## 2. Settings

- [ ] 2.1 Read `WEIR_WORKER_CONCURRENCY` in `weir serve`, refusing anything that is not a whole number from 1 to 256
- [ ] 2.2 Add `worker.concurrency` to the Helm chart, setting `WEIR_WORKER_CONCURRENCY`

## 3. Tests

- [ ] 3.1 Against Postgres: with a concurrency of N, N steps are running at the same moment, and every step runs exactly once
- [ ] 3.2 Against Postgres: with the default of 1, no two steps run at once
- [ ] 3.3 Against Postgres: a lease shorter than a step is kept alive for every concurrent step
- [ ] 3.4 Against Postgres: the sweep acts on an expired lease while every slot is busy
- [ ] 3.5 The setting's refusals, and the chart value

## 4. Finish

- [ ] 4.1 `README.md`, the chart's README and values
- [ ] 4.2 `docs/build/02-epics.md`
- [ ] 4.3 `proof.md` for `workers-and-tokens` and `deployment`
- [ ] 4.4 Archive the change, then `make check` and `make spec`
