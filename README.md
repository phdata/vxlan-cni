# Warning
Though functional, this software is still in an alpha state. Though written with compatibility in mind, this software has not been tested with IPv6.

# Description
This CNI plugin is for container runtime operators who wish to use subnets as another administrative boundary for applications running on their cluster. When using this plugin, every cluster node becomes a router/gateway to any number of layer 2 virtual broadcast domains which are spanned across the entire cluster. This allows you to logically separate applications by network, regardless of which nodes the containers making up the application run on.

The network to which to connect (and optionally with a specific address) is passed in as part of the configuration. Optionally, the plugin can look in Kubernetes for pod annotations for the necessary information, since kubernetes does not yet support passing annotations into a CNI plugin. This plugin will utilize an external CNI IPAM plugin, but it requires that the IPAM plugin is aware of all addresses cluster-wide. If you utilize the corresponding [routetable-ipam]() plugin, and a routing protocol, you can get efficient routing directly to a node running the destination container, without proxying through some other random node.
