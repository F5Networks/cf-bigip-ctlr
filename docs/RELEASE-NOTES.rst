Release Notes for BIG-IP Controller for Cloud Foundry
=====================================================

next-release
------------

Bug Fixes
`````````
* :cccl-issue:`208` - Address compatibility for BIG-IP v13.0 Health Monitor interval and timeout.

v1.1.0
------

Added Functionality
```````````````````
* Use the Controller as a Service Broker to apply per-route L7 (HTTP) configurations.

  * Virtual Server for L7 (HTTP) route can have its own Policies, Profiles and SSL Profiles.
  * Pool for L7 (HTTP) route can have its own load balancing mode and Health Monitors.

* Adopts a new, two-tier architecture. See the BIG-IP Controller for Cloud Foundry `user documentation`_ for more information.
* Support for JSESSIONID cookie session persistence.

Limitations
```````````
* Architected only as a Static, Brokered Service.
* Controller doesn't support Service bindings.
* Controller cannot accept Arbitrary Parameters via ``cf create-service|bind-service -c``.
* Controller doesn't support the use of ``cf update-service``.
* The BIG-IP data group cf-ctlr-data-group, found in the same partition that the controller manages, needs verification if the controller crashes. There should be a one to one mapping of tier 2 VIPs to data group records with the same name. If the record is missing for a VIP the controller will not rebuild the BIG-IP to the correct state it was in before the crash. This is because the controller crashed before the updated data group got written out. This condition requires manual intervention. Update the data group with the missing VIP's name as key and virtual address and port as data. The data must follow this format: {"bindAddr":"ADDRESS","port":PORT}. Encode the data as base64.
* JSESSIONID cookie session persistence will not work for BIG-IP v11.6.1 and will cause `Connection Reset by Peer` connection errors. Correct this by disabling session persistence, set session_persistence to false in the controller config.


v1.0.0
------

Added Functionality
```````````````````
* Support for JSESSIONID cookie session persistence.
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
