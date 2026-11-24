# Wait for a pull request to be merged

A run that stops at a step until GitHub says a pull request was closed, then goes one way if it was merged and another if it was not.

It shows an `external` step with `resume: event`: the step names the type of event it waits for, and `when` says which event is this run's.

## Run it

GitHub has to be able to reach Weir. Without that, post the event by hand, as below.

```bash
./bin/weir publish examples/wait-for-merge/workflow.yaml
./bin/weir run examples/wait-for-merge --input '{"pull_request": "https://github.com/acme/shop/pull/42"}'
```

The run waits. Stand in for GitHub:

```bash
curl -X POST "$WEIR_URL/v1/events" -H "Authorization: Bearer $WEIR_API_KEY" -H 'Content-Type: application/json' -d '{
  "type": "github.pull_request",
  "id": "by-hand-1",
  "data": {"action": "closed", "pull_request": {"html_url": "https://github.com/acme/shop/pull/42", "merged": true, "merged_by": {"login": "bob"}}}
}'
```

The reply lists the step under `resumed`, and the run finishes with `"result": "merged"`.

## What to notice

- An event about another pull request moves nothing: `when` is false for it.
- The event may arrive before the step starts waiting. The step looks back over events received since its run started.
- The step must set a timeout, so a pull request nobody closes does not hold the run for ever.
