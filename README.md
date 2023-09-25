# OpenShift Container Platform Release Payload Watcher

## Description

A reporting tool for checking on the health of OCP release streams.

By default information is pulled the [amd64 OCP release page](from https://amd64.ocp.releases.ci.openshift.org/).

## Reporting rules

The tool will report on each release stream (e.g. `4.12.0-0.ci`). The following conditions will be detected and reported on:

* Stream has not had a payload built recently
* Stream has not had a payload accepted recently
* Stream has not had a successful upgrade from a vN-1 minor recently
* Stream has not had a successful upgrade from an older 4.N.z recently

For each condition, the age at which a payload or upgrade edge is considered too old (stale) to count can be specified via arguments.

In practice the age at which payloads should be considered stale tends to increase for older release streams because we build them
less frequently and so it is more common that we don't have extremely recent (e.g. < 1 day) payloads to test.  It is not currently
possible to specify the staleness threshold on a per release stream basis, but this is on the roadmap to be added.

## Usage

```
$ go build .
$ ./release-watcher report

https://amd64.ocp.releases.ci.openshift.org/#4.14.0-0.nightly
  - Most recently accepted payload was 13.7 days ago, latest built payload is < 1.0 days old

https://amd64.ocp.releases.ci.openshift.org/#4.12.0-0.ci
  - Most recently accepted payload was 2.8 days ago, latest built payload is < 1.0 days old

https://amd64.ocp.releases.ci.openshift.org/#4.11.0-0.ci
  - Most recently accepted payload was 1.1 days ago, latest built payload is < 1.0 days old

https://amd64.ocp.releases.ci.openshift.org/#4.10.0-0.ci
  - Most recently accepted payload was 5.2 days ago, latest built payload is < 1.0 days old

https://amd64.ocp.releases.ci.openshift.org/#4.9.0-0.ci
  - Does not have a recent valid minor level upgrade
  - Most recently accepted payload was 7.3 days ago, latest built payload is < 1.0 days old

https://amd64.ocp.releases.ci.openshift.org/#4.9.0-0.nightly
  - Does not have a recent valid patch level upgrade
  - Does not have a recent valid minor level upgrade
  - Most recently built payload was 3.0 days ago
```

### Arguments

* --accepted-staleness-limit duration   How old an accepted payload can be before it is considered stale (default 24h0m0s)
* --built-staleness-limit duration      How old an built payload can be before it is considered stale (default 72h0m0s)
* --newest-minor int                    The newest minor release to analyze.  Release streams newer than this will be ignored.  Specify only the minor value (e.g. "12") (default to looking up the newest supported release)
* --oldest-minor int                    The oldest minor release to analyze.  Release streams older than this will be ignored.  Specify only the minor value (e.g. "9") (default to looking up the oldest supported release)
* --release-api-url string              The url of the release reporting api (default "https://amd64.ocp.releases.ci.openshift.org")
* --upgrade-staleness-limit duration    How old a successful upgrade attempt can be before it's considered stale (default 72h0m0s)


## TODO

* Specify staleness thresholds per release stream or automatically increase them for older releases
