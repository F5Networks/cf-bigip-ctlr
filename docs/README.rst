F5 BIG-IP Controller for Cloud Foundry
======================================

.. toctree::
    :hidden:
    :maxdepth: 2

    RELEASE-NOTES
    /_static/ATTRIBUTIONS

The |cfctlr-long| (|cfctlr|) lets you manage your F5 BIG-IP device from `Cloud Foundry`_ using the environment's native CLI/API.

|release-notes|

|attributions|

:fonticon:`fa fa-download` :download:`Attributions.md </_static/ATTRIBUTIONS.md>`

Features
--------

- Dynamically creates, manages, and destroys BIG-IP objects.
- Forwards traffic from the BIG-IP device to `Cloud Foundry`_ clouds via Diego cell virtual machine addressing.
- Supports Cloud Foundry HTTP and TCP routing.
- Supports use of `BIG-IP profiles`_ and `policies`_ with Cloud Foundry routes.

Guides
------

See the |cfctlr-long| `user documentation </containers/v1/cloudfoundry/index.html>`_.

Overview
--------

The |cfctlr| is a Docker container that runs in a `Cloud Foundry`_ cell.
It emulates the behavior of the Cloud Foundry `Gorouter`_, by:

- subscribing to the Cloud Foundry NATS message bus and routing API;
- gathering application route information (HTTP and TCP); and
- configuring routing policy rules on the BIG-IP system.

The |cfctlr| receives route updates and transforms them into BIG-IP policy rules when the following events occur:

- push, delete, and scale applications (with mapped routes to the application);
- map and unmap routes.

For example:

#. Developer pushes "*myApp*" to Cloud Foundry (*without the Cloud Foundry* ``--no-route`` *option*).
#. |cfctlr| discovers new route information for *myApp*.
#. Controller creates a pool and pool member(s) for each *myApp* instance.
#. Controller updates BIG-IP routing policy directing App requests to the *myApp* pool.
#. Controller monitors Cloud Foundry routing table and, when it discovers changes, reconfigures the BIG-IP device.

The BIG-IP device handles traffic for every Cloud Foundry mapped route and load balances traffic to each application instance.
The |cfctlr| can create a total of two (2) BIG-IP virtual servers for HTTP routes: one (1) for HTTP, one (1) for HTTPS.
The Controller creates an additional virtual server for each TCP route.

All HTTP traffic goes through either the port 80 or port 443 virtual server.
The Controller routes TCP traffic through dedicated virtual servers configured to listen on non-HTTP ports.

You can attach the BIG-IP objects listed below to the HTTP virtual servers the |cfctlr| creates for Cloud Foundry.
To do so, create the desired objects on the BIG-IP manually *before* adding them to the Controller configuration in Cloud Foundry.

- policies
- profiles
- SSL profiles
- health monitors

.. danger::
 
   The |cfctlr| monitors the BIG-IP partition it manages for configuration changes. If it discovers changes, the Controller reapplies its own configuration to the BIG-IP system.
   
   F5 does not recommend making configuration changes to objects in any partition managed by the |cfctlr| via any other means (for example, the configuration utility, TMOS, or by syncing configuration with another device or service group). Doing so may result in disruption of service or unexpected behavior.

.. _cfctlr-configuration:

Configuration Parameters
------------------------

The configuration parameters below customize the |cfctlr| behavior.
Define the parameters in the ``env`` section of your `application manifest </containers/v1/cloudfoundry/cfctlr-app-install.html>`_ using the environment variable ``BIGIP_CTLR_CFG``.

.. tip::

   It's possible to confuse the ``profiles`` configuration parameter with the ``ssl_profiles`` parameter.

   - ``profiles`` tells the |cfctlr| what `BIG-IP profiles`_ you want to attach to the virtual server(s) (for example, TCP acceleration or the ``X-Forwarded-For`` header).
   - ``ssl_profiles`` tells the |cfctlr| that it should create an HTTPS virtual server that uses the specified `BIG-IP SSL profiles`_.

\


+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| Parameter                                | Type    | Required | Default        | Description                                                                     | Allowed Values |
+==========================================+=========+==========+================+=================================================================================+================+
| .. _bigip-configs:                       |         |          |                |                                                                                 |                |
|                                          |         |          |                |                                                                                 |                |
| bigip                                    | object  | Required | n/a            | A YAML blob defining BIG-IP parameters.                                         |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | url                                 | string  | Required | n/a            | BIG-IP admin IP address                                                         |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | user                                | string  | Required | n/a            | BIG-IP iControl REST username                                                   |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | pass                                | string  | Required | n/a            | BIG-IP iControl REST password                                                   |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | partition                           | array   | Required | n/a            | The BIG-IP partition in which to configure objects.                             |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | balance                             | string  | Optional | round-robin    | Set the load balancing mode                                                     | Any BIG-IP-    |
|    |                                     |         |          |                |                                                                                 | supported mode |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | verify_interval                     | integer | Optional | 30             | In seconds; interval at which to verify the BIG-IP configuration.               |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | external_addr                       | string  | Required | n/a            | Virtual address on the BIG-IP to use for cloud ingress.                         |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | ssl_profiles                        | array   | Optional | n/a            | List of BIG-IP SSL policies to attach to the HTTPS routing virtual server.      |                |
|    |                                     |         |          |                | [#ssl]_                                                                         |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | policies                            | array   | Optional | n/a            | Additional pre-configured BIG-IP policies to attach to routing virtual servers  |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | profiles                            | array   | Optional | n/a            | Additional pre-configured BIG-IP profiles to attach to routing virtual servers  |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | health_monitors                     | array   | Optional | n/a            | Health monitors attached to each configured routing pool                        |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| status                                   | object  | Optional | n/a            | Basic authorization credentials for debug information                           |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | user                                | string  | Optional | n/a            | Status username                                                                 |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | pass                                | string  | Optional | n/a            | Status password                                                                 |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| .. _nats-configs:                        |         |          |                |                                                                                 |                |
|                                          |         |          |                |                                                                                 |                |
| nats                                     | array   | Required | n/a            | NATS message bus                                                                |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | host                                | string  | Required | n/a            | NATS host                                                                       |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | port                                | integer | Required | n/a            | NATS port                                                                       |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | user                                | string  | Required | n/a            | NATS username                                                                   |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
|    | pass                                | string  | Required | n/a            | NATS password                                                                   |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| .. _logging-configs:                     |         |          |                |                                                                                 |                |
|                                          |         |          |                |                                                                                 |                |
| logging                                  | object  | Optional | n/a            | Logging configuration                                                           |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
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
| .. _oauth-configs:                       |         |          |                |                                                                                 |                |
|                                          |         |          |                |                                                                                 |                |
| oauth                                    | object  | Optional | n/a            | UAA token server configuration                                                  |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
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
| .. _routing-api-configs:                 |         |          |                |                                                                                 |                |
|                                          |         |          |                |                                                                                 |                |
| routing_api                              | object  | Optional | n/a            | Routing API configuratoin                                                       |                |
+----+-------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
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
| suspend_prune_if_nats_unavailable        | boolean | Optional | false          | If NATS becomes unavailable should pruning suspend                              |                |
+------------------------------------------+---------+----------+----------------+---------------------------------------------------------------------------------+----------------+
| route_mode                               | string  | Optional | http           | :ref:`Route type <route types>` you want to watch; must be a single value       | http, tcp, all |
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

\

.. [#ssl] SSL profiles must already exist on the BIG-IP device in a partition accessible by the |cfctlr| (for example, :code:`/Common`).

.. _cfctlr-config-examples:

Configuration Examples
``````````````````````

The example |cfctlr| application manifest below defines the following:

- Cloud Foundry health check;
- BIG-IP URL;
- BIG-IP login credentials;
- BIG-IP partition the |cfctlr| should manage;
- BIG-IP virtual IP address that should serve as the cloud ingress;
- Cloud Foundry NATS message bus to subscribe to for routing information;
- Cloud Foundry NATS message bus login credentials;
- Cloud Foundry OAuth endpoint for API access;
- Cloud Foundry OAuth API access credentials;
- Cloud Foundry routing API endpoint;
- Type of routes to watch in Cloud Foundry (all routes, in this case).

.. literalinclude:: /_static/config_examples/example-manifest.yml

- :fonticon:`fa fa-download` :download:`sample-cf-app-manifest.yml </_static/config_examples/example-manifest.yml>`
- :fonticon:`fa fa-download` :download:`sample-cf-config-structure.yml </_static/config_examples/config-structure.yml>`

.. _route types:

Route Types
-----------

The |cfctlr| accepts three (3) values for the ``route_type`` configuration parameter.
Each of the supported ``route_mode`` options have different configuration requirements:

- ``all``: Requires :ref:`nats <nats-configs>`, :ref:`routing_api <routing-api-configs>`, and :ref:`oauth <oauth-configs>`.
- ``http``: Requires :ref:`nats <nats-configs>`.
- ``tcp``: Requires :ref:`routing_api <routing-api-configs>` and :ref:`oauth <oauth-configs>`.

When managing HTTP routes, the |cfctlr| will create an HTTP virtual server (virtual address port 80) for routing into `Cloud Foundry`_.
If you define an SSL profile in the configuration (the ``ssl_profiles`` parameter), the Controller creates an additional HTTPS virtual server (virtual address port 443).
You can attach multiple certificate/key pairs to the HTTPS virtual server.
The BIG-IP device uses `TLS Server Name Indication`_ (SNI) to choose the correct certificate to present to the client; SNI allows the `Cloud Foundry`_ instance to support multiple hostnames (foo.mycf.com and bar.mycf.com).
Some of these cert/key pairs can be wildcard (\*.mycf.com).

.. _health checks:

Health Checks
-------------

The |cfctlr| supports native Cloud Foundry health checks for itself and will manage BIG-IP health checks for applications.

To configure Cloud Foundry health checks for the |cfctlr| (this ensures the orchestration system will manage the Controller availability for you), define the ``health-check-type`` and ``health-check-http-endpoint`` settings as follows:

.. code-block:: yaml

   health-check-type: http
   health-check-http-endpoint: /health

The |cfctlr| will also manage BIG-IP health checking of the managed applications. To use any health monitor(s) that already exists on the BIG-IP system, add the name to the application manifest under ``bigip.health_monitors``. Because these monitors apply to all applications in the system, the |cfctlr| uses the ``/Common/tcp_half_open`` monitor by default.

API Endpoints
-------------

- :code:`/health`: The Controller health endpoint.

  The Controller returns :code:`200 OK` to indicate health; any other response is unhealthy.
  You can set the health Controller endpoint to the `status.port` property in the application configuration for development purposes. The Diego PORT value provided to the container environment will override this setting in production environments.

  .. code-block:: bash

     curl -v http://10.0.32.15/health
     *   Trying 10.0.32.15..
     * Connected to 10.0.32.15 (10.0.32.15) port 80 (#0)
     > GET /health HTTP/1.1
     > Host: 10.0.32.15
     > User-Agent: curl/7.43.0
     > Accept: */*
     >
     < HTTP/1.1 200 OK
     < Cache-Control: private, max-age=0
     < Expires: 0
     < Date: Thu, 22 Sep 2016 00:13:54 GMT
     < Content-Length: 3
     < Content-Type: text/plain; charset=utf-8
     <
     ok
     * Connection #0 to host 10.0.32.15 left intact

- :code:`/routes`: The routes endpoint returns the entire routing table as JSON. Each route has an associated array of ``host:port`` entries.

  .. code-block:: bash
     :caption: Example

     curl "http://someuser:somepass@10.0.32.15/routes"
     {"api.catwoman.cf-app.com":[{"address":"10.244.0.138:9022","ttl":0,"tags":{"component":"CloudController"}}],"dora-dora.catwoman.cf-app.com":[{"address":"10.244.16.4:60035","ttl":0,"tags":{"component":"route-emitter"}},{"address":"10.244.16.4:60060","ttl":0,"tags":{"component":"route-emitter"}}]}

  .. important::

     Use of ``/routes`` requires HTTP basic authentication.
     You can find the authentication credentials in the application configuration:

     .. code-block:: yaml

        status:
           password: some_password
           user: some_user

.. instrumentation and logging:

Instrumentation and Logging
---------------------------

You can define the desired :ref:`logging level <logging-configs>` for the |cfctlr| in the application manifest.
The Controller supports the following log levels:

* ``info``, ``debug`` - An expected event occurred.
* ``error`` - An unexpected error occurred.
* ``fatal`` - A fatal error occured which makes the Controller unable to execute.

.. code-block:: text
   :caption: Sample log message

   [2017-02-01 22:54:08+0000] {"log_level":0,"timestamp":1485989648.0895808,"message":"endpoint-registered","source":"vcap.cf-bigip-ctlr.registry","data":{"uri":"0-*.login.bosh-lite.com","backend":"10.123.0.134:8080","modification_tag":{"guid":"","index":0}}}


- ``log_level``: message log level

  - 0 = info
  - 1 = debug
  - 2 = error
  - 3 = fatal

- ``timestamp``: Epoch time of the log
- ``message``: Content of the log line
- ``source``: The Controller function that initiated the log message
- ``data``: Additional information, varies based on the message

.. _Cloud Foundry: https://cloudfoundry.org/
.. _Gorouter: https://github.com/cloudfoundry/gorouter
.. _TLS Server Name Indication: https://tools.ietf.org/html/rfc6066#section-3
.. _Deploying with Application Manifests: https://docs.cloudfoundry.org/devguide/deploy-apps/manifest.html
.. _BIG-IP profiles: https://support.f5.com/kb/en-us/products/big-ip_ltm/manuals/product/ltm-profiles-reference-13-0-0.html
.. _policies: https://support.f5.com/kb/en-us/products/big-ip_ltm/manuals/product/bigip-local-traffic-policies-getting-started-13-0-0.html
.. _BIG-IP SSL profiles: https://support.f5.com/kb/en-us/products/big-ip_ltm/manuals/product/ltm-profiles-reference-13-0-0/6.html
