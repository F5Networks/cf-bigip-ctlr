Release Notes for BIG-IP Controller for Cloud Foundry
=====================================================

|release|
---------

Added Functionality
^^^^^^^^^^^^^^^^^^^
* Users can specify their own custom policy, profile, or health monitor to associate with the objects created on the BIG-IP
* Manages the following Local Traffic Manager (LTM) resources for the BIG-IP partition

  * Virtual Servers
  * Pools
  * Pool members
  * Nodes
  * Policies

    * Rules

      * Actions
      * Conditions

Limitations
^^^^^^^^^^^
* The configurations defined in the application manifest apply to all BIG-IP LTM objects created in the designated partition for Cloud Foundry. 
* If using HTTPS, the SSL profile(s) defined in the application manifest only attach to the HTTPS virtual server.
* The BIG-IP Controller only controls one partition on the BIG-IP device.
* Version |release| does not support TCP routing.
