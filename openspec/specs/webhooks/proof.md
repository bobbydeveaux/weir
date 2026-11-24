# Proof: webhooks

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A connection receives webhooks at an address of its own | `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` |
| The signature is the credential | `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` |
| GitHub's signature is checked as GitHub sends it | `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` (against requests signed in the test, not the real service) |
| A general kind checks any sender that signs with HMAC-SHA256 | `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` (the signature, event and id headers are asserted; `prefix` and a fixed `event` are not) |
| A refused request records nothing | `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` (the 401s, the 404s and a missing event name are asserted; a body that is not JSON and one over 5 MB are not) |
| A verified webhook becomes an event | `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` (an event name is not sent in capitals, so the lower-casing is not asserted) |
| The delivery id makes a repeat harmless | `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` |
| One workspace's secret does not verify another's webhooks | `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` |
| A webhook whose secret is not set is refused | `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` |
