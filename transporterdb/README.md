# Integration tests of DB

Run Postgres:

```
docker run --rm --name test-instance -e POSTGRES_PASSWORD=password -p 127.0.0.1:5432:5432 postgres
```

Run tests:

```
POSTGRES_CONFIG=./integration_test/postgres_docker.toml go test -tags integration_test
```
