Release Notes for BIG-IP Controller for Cloud Foundry
=====================================================

|release|
---------

Added Functionality
```````````````````

v1.1.0
------

Added Functionality
```````````````````
* Use the Controller as a Service Broker to apply per-route L7 (HTTP) configurations.

  * Virtual Server for L7 (HTTP) route can have its own Policies, Profiles and SSL Profiles.
  * Pool for L7 (HTTP) route can have its own load balancing mode and Health Monitors.

* Adopts a new, two-tier architecture. See the BIG-IP Controller for Cloud Foundry documentation </containers/latest/cloudfoundry/#overview>_ for more information.

Limitations
```````````
* Architected only as a Static, Brokered Service.
* Controller doesn't support Service bindings.
* Controller cannot accept Arbitrary Parameters via `cf create-service|bind-service -c`.
* Controller doesn't support the use of `cf update-service`.

v1.0.0
------

Added Functionality
```````````````````
* Support for TCP and HTTP routing.
* Attach custom policy, profile, or health monitor to L7 objects created on the BIG-IP device.
* Manages the following Local Traffic Manager (LTM) resources for the BIG-IP partition:

  * Virtual Servers
  * Pools
  * Pool members
  * Nodes
  * Policies

    * Rules

      * Actions
      * Conditions

Limitations
```````````
* The BIG-IP Controller controls one (1) partition on the BIG-IP device.
* Controller configurations are global: they apply to all L7 (HTTP) LTM objects in the designated BIG-IP partition.
* This release supports custom policies and profiles for **L7 virtual servers** only.
* Configured health monitor objects apply to all pools (both L4 and L7 routes).
* SSL profile(s) defined in the application manifest do not attach to the HTTP virtual server.
* Modification of a Controller-owned policy resulting in a state change may cause traffic flow interruptions. If the modification changes the state to ‘published’, the Controller will delete the policy and recreate it with a ‘legacy’ status.
* You cannot change the default route domain for a partition managed by an F5 controller after the controller has deployed. To specify a new default route domain, use a different partition.
