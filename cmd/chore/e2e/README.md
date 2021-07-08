# Running e2e tests

Manually triggering an e2e-test:
```bash
go run ./cmd/chore/e2e/trigger/*.go --organization caos --repository ORBOS-Test-StaticProvider --access-token $GITHUB_ACCESS_TOKEN --testcase staticprovider --cleanup=true --from 1
go run ./cmd/chore/e2e/trigger/*.go --organization caos --repository ORBOS-Test-GCEProvider --access-token $GITHUB_ACCESS_TOKEN --testcase gceprovider --cleanup=true --from 1
```