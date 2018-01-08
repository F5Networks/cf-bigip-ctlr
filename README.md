[![Build Status](https://travis-ci.org/F5Networks/cf-bigip-ctlr.svg?branch=master)](https://travis-ci.org/F5Networks/cf-bigip-ctlr) [![Slack](https://f5cloudsolutions.herokuapp.com/badge.svg)](https://f5cloudsolutions.herokuapp.com) [![Coverage Status](https://coveralls.io/repos/github/F5Networks/cf-bigip-ctlr/badge.svg?branch=HEAD)](https://coveralls.io/github/F5Networks/cf-bigip-ctlr?branch=HEAD)

F5 BIG-IP Controller for Cloud Foundry
======================================

The F5 BIG-IP Controller for [Cloud Foundry](http://cloudfoundry.org) makes the F5 BIG-IP
[Local Traffic Manager](<https://f5.com/products/big-ip/local-traffic-manager-ltm)
services available to applications running in the Cloud Foundry platform.

Documentation
-------------

For instructions on how to use this component, use the
[F5 BIG-IP Controller for Cloud Foundry docs](http://clouddocs.f5.com/products/connectors/cf-bigip-ctlr/latest/).

For guides on this and other solutions for Cloud Foundry, see the
[F5 Solution Guides for Cloud Foundry](http://clouddocs.f5.com/containers/latest/cloudfoundry).

Getting Help
------------

We encourage you to use the cc-cloudfoundry channel in our [f5CloudSolutions Slack workspace](https://f5cloudsolutions.herokuapp.com/) for discussion and assistance on this
controller. This channel is typically monitored Monday-Friday 9am-5pm MST by F5
employees who will offer best-effort support.

Contact F5 Technical support via your typical method for more time sensitive
changes and other issues requiring immediate support.

Running
-------

The official docker image is `f5networks/cf-bigip-ctlr`.

Usually, the controller is deployed in Cloud Foundry. However, the controller can be run locally for development testing.
The controller requires a running NATS server, without a valid connection the controller will not start. The controller
can either be run against a gnatsd server or standalone against a Cloud Foundry installation.

Option gnatsd:
- Go should be installed and in the PATH
- GOPATH should be set as described in http://golang.org/doc/code.html
- Pip install the python/cf-runtime-requirements.txt into a virtualenv of your choice
```bash
workon cf-bigip-ctlr
pip install -r python/cf-runtime-requirements.txt
```
- [gnatds](https://github.com/nats-io/gnatds) installed and in the PATH
```
go get github.com/nats-io/gnatsd
gnatsd &
```
- Optionally run unit tests
```
go get github.com/onsi/ginkgo
go get github.com/onsi/gomega
ginkgo -keepGoing -trace -p -progress -r -failOnPending -randomizeAllSpecs -race
```
- Build and install controller from a cloned [cf-bigip-ctlr](https://github.com/F5Networks/cf-bigip-ctlr)
```
go install
```
- Update BIGIP_CTLR_CFG environment variable for your specific environment
  as described in "Configuration"
- Run the controller
```
cf-bigip-ctlr
```

Option standalone:
- Go should be installed and in the PATH
- GOPATH should be set as described in http://golang.org/doc/code.html
- Pip install the python/cf-runtime-requirements.txt into a virtualenv of your choice
```bash
workon cf-bigip-ctlr
pip install -r python/cf-runtime-requirements.txt
```
- Optionally run unit tests
```
go get github.com/onsi/ginkgo
go get github.com/onsi/gomega
ginkgo -keepGoing -trace -p -progress -r -failOnPending -randomizeAllSpecs -race
```
- Build and install controller from a cloned [cf-bigip-ctlr](https://github.com/F5Networks/cf-bigip-ctlr)
```
go install
```
- Update configuration file or BIGIP_CTLR_CFG environment variable for your specific environment
  as described in "Configuration"
- Run the controller
```
cf-bigip-ctlr -c [CONFIG_FILE]
```

Building
--------

The official images are built using docker, but standard go build tools can be used for development
purposes as described above.

### Official Build

Prerequisites:
- Docker

```bash
git clone https://github.com/F5Networks/cf-bigip-ctlr.git
cd cf-bigip-ctlr

# Use docker to build the release artifacts into a local "_docker_workspace" directory and push into docker images
make prod
```

### Alternate, unofficial build

A normal go and godep toolchain can be used as well

Prerequisites:
- go 1.7
- GOPATH pointing at a valid go workspace
- godep (Only needed to modify vendor's packages)
- python
- virtualenv

```bash
mkdir -p $GOPATH/src/github.com/F5Networks
cd $GOPATH/src/github.com/F5Networks
git clone https://github.com/F5Networks/cf-bigip-ctlr.git
cd cf-bigip-ctlr

# Building all packages, and run unit tests
make prod
```

Configuration
-------------

When pushing the controller into a Cloud Foundry environment a configuration must be passed
via the application manifest. An example manifest is located in the
`docs/_static/config_examples` directory.

Update required sections for environment:
- nats: leave empty for gnatsd otherwise update with CF installed NATS information
- bigip: leave empty if no BIG-IP is required otherwise update with BIG-IP information
- routing_api: only required if routing API access is required (TCP routing with route_mode
set to all, or tcp)
- oauth: only required if routing API access is required (TCP routing with route_mode
set to all, or tcp)

On startup, the controller will get the `BIGIP_CTLR_CFG` variable from the environment;
setting this variable via an application manifest is required, environment parameters
are set in the manifest `env` section. The controller also supports Cloud Foundry health
checks. These can be configured in the manifest fields: `health-check-type`, and
`health-check-http-endpoint`. In order to configure health checking of the controller use
these settings:

```
health-check-type: http
health-check-http-endpoint: /health
```

To explore what other settings are available refer to the Cloud Foundry documentation
[Deploying with Application Manifests](https://docs.cloudfoundry.org/devguide/deploy-apps/manifest.html).

A minimal configuration manifest to support HTTP routing mode would be written as such
(`route_mode` set to 'http'):
```
applications:
  - name: cf-bigip-ctlr
    health-check-type: http
    health-check-http-endpoint: /health
    env:
      BIGIP_CTLR_CFG: |
                      bigip:
                        url: https://bigip.example.com
                        user: admin
                        pass: password
                        partition:
                          - example
                        external_addr: 192.168.1.1
                      nats:
                        - host: 192.168.10.1
                          port: 4222
                          user: nats
                          pass: nats-password
                      route_mode: http
```

If TCP routing is required, the `oauth` and `routing_api` sections are required
but not the `nats` section. Set `route_mode` to 'tcp'.

If both modes are required, the `oauth`, `routing_api`, and `nats` sections are required.
Set `route_mode` to 'all'.

Service Broker
--------------

If per route config is required the controller can be ran as a [service broker.](https://docs.cloudfoundry.org/services/overview.html) Running as a service
broker allows control over which BIG-IP objects are applied to a particular route through the use of plans. For more information see [F5 BIG-IP Controller for Cloud Foundry docs](http://clouddocs.f5.com/products/connectors/cf-bigip-ctlr/latest/).

An additional variable of `SERVICE_BROKER_CONFIG` will need to be added under the `env` section.

```
applications:
  - name: cf-bigip-ctlr
    health-check-type: http
    health-check-http-endpoint: /health
    env:
      SERVICE_BROKER_CONFIG: |
                            plans:
                              - description: plan for policy A,
                                name: planA,
                                virtualServer:
                                - policies:
                                  - policyA


```

Development
-----------

**Note**: This repository should be imported as `github.com/F5Networks/cf-bigip-ctlr`.

## Dynamic Routing Table

The controller's routing table is updated dynamically via the NATS message bus.
NATS can be deployed via BOSH with
([cf-release](https://github.com/cloudfoundry/cf-release)) or standalone using
[nats-release](https://github.com/cloudfoundry/nats-release).

To add or remove a record from the routing table, a NATS client must send
register or unregister messages. Records in the routing table have a maximum
TTL of 120 seconds, so clients must heartbeat registration messages
periodically; we recommend every 20s. [Route
Registrar](https://github.com/cloudfoundry/route-registrar) is a BOSH job that
comes with [Routing
Release](https://github.com/cloudfoundry-incubator/routing-release) that
automates this process.

When deployed with Cloud Foundry, registration of routes for apps pushed to CF
occurs automatically without user involvement. For details, see [Routes and
Domains](https://docs.cloudfoundry.org/devguide/deploy-apps/routes-domains.html).

### Registering Routes via NATS

When the controller starts, it sends a `router.start` message to NATS. This
message contains an interval that other components should then send
`router.register` on, `minimumRegisterIntervalInSeconds`. It is recommended
that clients should send `router.register` messages on this interval. This
`minimumRegisterIntervalInSeconds` value is configured through the
`start_response_delay_interval` configuration property. The controller will prune
routes that it considers to be stale based upon a seperate "staleness" value,
`droplet_stale_threshold`, which defaults to 120 seconds. The controller will check
if routes have become stale on an interval defined by
  `prune_stale_droplets_interval`, which defaults to 30 seconds. All of these
  values are represented in seconds and will always be integers.

The format of the `router.start` message is as follows:

```json
{
  "id": "some-router-id",
  "hosts": ["1.2.3.4"],
  "minimumRegisterIntervalInSeconds": 20,
  "prunteThresholdInSeconds": 120,
}
```

After a `router.start` message is received by a client, the client should send
`router.register` messages. This ensures that the new controller can update its
routing table.

If a component comes online after the controller, it must make a NATS request
called `router.greet` in order to determine the interval. The response to this
message will be the same format as `router.start`.

The format of the `router.register` message is as follows:

```json
{
  "host": "127.0.0.1",
  "port": 4567,
  "uris": [
    "my_first_url.vcap.me",
    "my_second_url.vcap.me"
  ],
  "tags": {
    "another_key": "another_value",
    "some_key": "some_value"
  },
  "app": "some_app_guid",
  "stale_threshold_in_seconds": 120,
  "private_instance_id": "some_app_instance_id",
  "router_group_guid": "some_router_group_guid"
}
```

`stale_threshold_in_seconds` is the custom staleness threshold for the route
being registered. If this value is not sent, it will default to the controller's
default staleness threshold.

`app` is a unique identifier for an application that the endpoint is registered
for.

`private_instance_id` is a unique identifier for an instance associated with
the app identified by the `app` field.

`router_group_guid` determines which controllers will register route. Only
controllers configured with the matching router group will register the route. If
a value is not provided, the route will be registered by all controllers that
have not be configured with a router group.

Such a message can be sent to both the `router.register` subject to register
URIs, and to the `router.unregister` subject to unregister URIs, respectively.

**Note:** In order to use `nats-pub` to register a route, you must run the command on the NATS VM. If you are using [`cf-deployment`](https://github.com/cloudfoundry/cf-deployment), you can run `nats-pub` from any VM.
