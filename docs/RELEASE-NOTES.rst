Release Notes for BIG-IP Controller for Cloud Foundry
=====================================================

v1.0.0-beta.1
-------------

Added Functionality
```````````````````

- User can specify their own custom policy, profile or health monitor to associate with the objects created on the BIG-IP
- Manages the following LTM resources for the BIG-IP partition

  - Virtual Servers
  - Pools
  - Pool members
  - Nodes
  - Policies

    - Rules

      - Actions
      - Conditions

Limitations
```````````

- All configurations defined in the manifest apply to all objects created in the designated partition for Cloud Foundry on the BIG-IP device

  - Exception to this is the ssl profile, this is only attached to the https virtual server

- Controller only controls one partition on the BIG-IP
- Does not support TCP routing
