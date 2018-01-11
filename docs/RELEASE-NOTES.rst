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
* L7 (HTTP) per route configuration through Route Services
* Ability to run as a Service Broker to enable per route configuration

  * Virtual Server for L7 (HTTP) route can have its own Policies, Profiles and SSL Profiles
  * Pool for L7 (HTTP) route can have its own load balancing mode and Health Monitors

* Virtual Server targeting Virtual Server architecture

Limitations
```````````
* Only support Route Service bindings
* Architected only as a Static, Brokered Service
* Do not accept any Arbitrary Parameters via `cf create-service|bind-service -c`
* Do not support `cf update-service`

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
