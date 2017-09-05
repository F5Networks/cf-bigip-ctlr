F5 BIG-IP Controller for Cloud Foundry
======================================

.. toctree::
    :hidden:
    :maxdepth: 2

    RELEASE-NOTES
    /_static/ATTRIBUTIONS

The |cfctlr-long| (|cfctlr|) manages F5 BIG-IP `Local Traffic Manager <https://f5.com/products/big-ip/local-traffic-manager-ltm>`_ (LTM) objects from `Cloud Foundry`_.

|release-notes|

|attributions|

:fonticon:`fa fa-download` :download:`Attributions.md </_static/ATTRIBUTIONS.md>`

Features
--------
- Dynamically creates, manages, and destroys BIG-IP objects.
- Forwards traffic from BIG-IP to `Cloud Foundry`_ clouds via Diego cell virtual machine addressing.
- Support for Cloud Foundry HTTP routing.
- Support for pre-configured BIG-IP policies and profiles.

Guides
------

See the `F5 Container Connector for Cloud Foundry user documentation </containers/v1/cloudfoundry/index.html>`_.

Overview
--------

The |cfctlr-long| is a Docker container that runs in a `Cloud Foundry`_ cell.
It subscribes to the Cloud Foundry NATS message bus and routing API; gathers application route information; and configures the BIG-IP device with a routing policy, emulating the behavior of the Cloud Foundry `Gorouter`_.

The |cfctlr-long| receives route updates and transforms them into BIG-IP policies when the following events occur in Cloud Foundry:

- push, delete, and scale applications;
- create, delete, map, and unmap routes.

For example:

#. Developer pushes fooApp to Cloud Foundry.
#. |cfctlr-long| discovers new route information for fooApp.
#. |cfctlr-long| creates a pool and pool member(s) for each fooApp instance.
#. |cfctlr-long| updates BIG-IP routing policy directing fooApp requests to the fooApp pool
#. |cfctlr-long| monitors Cloud Foundry routing table and reconfigures the BIG-IP device when it discovers changes.

The BIG-IP device handles traffic for every Cloud Foundry route and application and load balances to each application instance.
All route domains are global.
The |cfctlr-long| can create a total of two (2) BIG-IP virtual servers:

- one (1) for http
- one (1) for https.

The virtual server contains pools and pool members for each application and application instance.
You can define policies, profiles, and health monitors on the BIG-IP device in advance and apply them to the virtual server created by |cfctlr|.

.. _cfctlr-configuration:

Configuration Parameters
------------------------

+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| Parameter                                | Type    | Required | Default        | Description                                                                     | Allowed Values |
+==========================================+=========+==========+================+=================================================================================+================+
| bigip                                    | object  | Required | n/a            | A YAML blob defining BIG-IP parameters.                                         |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | url                                 | string  | Required | n/a            | BIG-IP admin IP address                                                         |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | user                                | string  | Required | n/a            | BIG-IP iControl REST username                                                   |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | partition                           | array   | Required | n/a            | The BIG-IP parition in whichto configure objects.                               |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | balance                             | string  | Optional | round-robin    | Set the load balancing mode                                                     | Any supported  |
|    |                                     |         |          |                |                                                                                 | BIG-IP mode    |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | verify_interval                     | integer | Optional | 30             | In seconds, interval at which to verify the BIG-IP configuration                |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | external_addr                       | string  | Required | n/a            | Virtual address from the BIG-IP, this is the cloud ingress address              |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | policies                            | array   | Optional | n/a            | Additional pre-configured BIG-IP policies to attach to routing virtual servers  |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | profiles                            | array   | Optional | n/a            | Additional pre-configured BIG-IP profiles to attach to routing virtual servers  |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | health_monitors                     | array   | Optional | n/a            | Health monitors attached to each configured routing pool                        |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| status                                   | object  | Optional | n/a            | Basic authorization credentials for debug and health information                |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | user                                | string  | Optional | n/a            | Status username                                                                 |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | pass                                | string  | Optional | n/a            | Status password                                                                 |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| nats                                     | array   | Required | n/a            | NATS message bus                                                                |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | host                                | string  | Required | n/a            | NATS host                                                                       |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | port                                | integer | Required | n/a            | NATS port                                                                       |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | user                                | string  | Required | n/a            | NATS username                                                                   |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | pass                                | string  | Required | n/a            | NATS password                                                                   |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| logging                                  | object  | Optional | n/a            | Logging configuration                                                           |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | file                                | string  | Optional | n/a            | Logging file name                                                               |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | syslog                              | string  | Optional | n/a            | Syslog ID                                                                       |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | level                               | string  | Optional | debug          | Logging level                                                                   |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | loggregator_enabled                 | boolean | Optional | false          | Is loggregator facility enabled                                                 |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | metron_address                      | string  | Optional | localhost:3457 | Metron address                                                                  |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| oauth                                    | object  | Optional | n/a            | UAA token server configuration                                                  |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | token_endpoint                      | string  | Optional | n/a            | UAA token server                                                                |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | client_name                         | string  | Optional | n/a            | UAA username                                                                    |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | client_secret                       | string  | Optional | n/a            | UAA password                                                                    |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | port                                | string  | Optional | n/a            | UAA listen port                                                                 |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | skip_ssl_validation                 | boolean | Optional | false          | Should skip SSL verification                                                    |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | ca_certs                            | string  | Optional | n/a            | CA cert bundle                                                                  |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| routing_api                              | object  | Optional | n/a            | Routing API configuratoin                                                       |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | uri                                 | string  | Optional | n/a            | Routing API endpoint                                                            |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | port                                | integer | Optional | n/a            | Routing API listen port                                                         |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | auth_disabled                       | boolean | Optional | false          | Routing API authorization status                                                |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| go_max_procs                             | integer | Optional | -1             | Golang GOMAXPROCS limits                                                        |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| prune_stale_droplets_interval            | integer | Optional | 30             | In seconds, interval to check and prune stale routes                            |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| droplet_stale_threshold                  | integer | Optional | 120            | In seconds, threshold to consider route stale                                   |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| suspend_prune_if_nats_unavailable        | boolean | Optional | false          | If NATS becomes unaviable should pruning suspend                                |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| route_mode                               | string  | Optional | http           | Route types you want to watch, provide a single value                           | http, tcp, all |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| start_response_delay_interval            | integer | Optional | 5              | In seconds, wait time to achieve steady state from routing message bus          |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| token_fetcher_max_retries                | integer | Optional | 3              | Number of retries to fetch auth token                                           |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| token_fetcher_retry_interval             | integer | Optional | 5              | In seconds, time to wait between token fetch retries                            |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| token_fetcher_expiration_buffer_time     | integer | Optional | 30             | In seconds, time to re-fetch auth token                                         |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| tcp_router_group                         | string  | Optional | default-tcp    | Name of TCP router group                                                        |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+

.. _cfctrl-config-examples:

Example Configuration Files
```````````````````````````

- :fonticon:`fa fa-download` :download:`sample-cf-manifest-structure.yml </_static/config_examples/manifest-structure.yml>`
- :fonticon:`fa fa-download` :download:`sample-cf-config-structure.yml </_static/config_examples/config-structure.yml>`
- :fonticon:`fa fa-download` :download:`sample-cf-app-manifest.yml </_static/config_examples/example-manifest.yml>`

API Endpoints
-------------

:code:`/health`: The controller health endpoint. The controller returns :code:`200 OK` to indicate health; any other response is unhealthy.
:code:`/routes`: The routes endpoint returns the entire routing table as JSON. Each route has an associated array of host:port entries.

.. important::

   Both endpoints require basic authentication.

.. _Cloud Foundry: http://cloudfoundry.org/
.. _Gorouter: https://github.com/cloudfoundry/gorouter
